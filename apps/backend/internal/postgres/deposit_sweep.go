package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// maxDepositLastErrorLen bounds deposits.last_error so an upstream body cannot bloat the row.
const maxDepositLastErrorLen = 500

// SweepDepositRow is a pending deposit claimed by one sweep poller, with its retry state.
type SweepDepositRow struct {
	DepositRow
	AttemptCount         int
	SweepSubmitCount     int
	LastValidBlockHeight sql.NullInt64
}

// ClaimNextSweepDeposit leases the most overdue pending deposit to owner, or found=false when
// nothing is due. FOR UPDATE SKIP LOCKED plus the lease keep two pollers (or two API
// processes) off the same row; a lease older than leaseFor belongs to a crashed owner and is
// reclaimable. excludeIDs are deposits this tick already handled.
// Faker users and faker groups (#153) are excluded: their rows must never reach Privy/RPC.
func (s *Store) ClaimNextSweepDeposit(ctx context.Context, owner string, now time.Time, leaseFor time.Duration, excludeIDs []string) (SweepDepositRow, bool, error) {
	if owner == "" {
		return SweepDepositRow{}, false, fmt.Errorf("claim owner is required")
	}
	if leaseFor <= 0 {
		return SweepDepositRow{}, false, fmt.Errorf("lease duration must be positive")
	}
	if excludeIDs == nil {
		excludeIDs = []string{}
	}

	// The due row is materialized so the LIMIT subquery runs (and locks) exactly once.
	const claimSQL = `
WITH due AS MATERIALIZED (
  SELECT d.id
  FROM deposits d
  JOIN users u ON u.id = d.user_id
  JOIN groups g ON g.id = d.group_id
  WHERE d.status = 'pending' AND NOT u.is_faker AND NOT g.is_faker
    AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= $2)
    AND (d.claimed_at IS NULL OR d.claimed_at <= $3)
    AND NOT (d.id = ANY($4::uuid[]))
  ORDER BY d.next_attempt_at ASC NULLS FIRST, d.created_at ASC
  LIMIT 1
  FOR UPDATE OF d SKIP LOCKED
)
UPDATE deposits
SET claimed_at = $2, claimed_by = $1
FROM due
WHERE deposits.id = due.id
RETURNING deposits.id, deposits.user_id, deposits.group_id, deposits.amount, deposits.from_address,
  deposits.status, deposits.tx_signature, deposits.created_at,
  deposits.attempt_count, deposits.sweep_submit_count, deposits.sweep_last_valid_block_height`

	now = now.UTC()
	var row SweepDepositRow
	err := s.db.QueryRowContext(ctx, claimSQL, owner, now, now.Add(-leaseFor), excludeIDs).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
		&row.AttemptCount,
		&row.SweepSubmitCount,
		&row.LastValidBlockHeight,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SweepDepositRow{}, false, nil
	}
	if err != nil {
		return SweepDepositRow{}, false, fmt.Errorf("claim sweep deposit: %w", err)
	}
	return row, true, nil
}

// ReleaseSweepDeposit drops owner's lease so the deposit is claimable on the next tick.
func (s *Store) ReleaseSweepDeposit(ctx context.Context, depositID, owner string) error {
	if depositID == "" || owner == "" {
		return fmt.Errorf("deposit id and claim owner are required")
	}

	const releaseSQL = `
UPDATE deposits
SET claimed_at = NULL, claimed_by = NULL
WHERE id = $1 AND claimed_by = $2`

	if _, err := s.db.ExecContext(ctx, releaseSQL, depositID, owner); err != nil {
		return fmt.Errorf("release sweep deposit: %w", err)
	}
	return nil
}

// RecordSweepSignature stores the sweep signature and its expiry height BEFORE the transaction
// is broadcast, so a crash after broadcast can never lead to a second sweep. recorded=false
// means owner lost the lease, the deposit left pending, or a signature is already outstanding:
// the caller must not broadcast.
func (s *Store) RecordSweepSignature(ctx context.Context, depositID, owner, txSignature string, lastValidBlockHeight sql.NullInt64) (bool, error) {
	if depositID == "" || owner == "" || txSignature == "" {
		return false, fmt.Errorf("deposit id, claim owner and tx signature are required")
	}

	const updateSQL = `
UPDATE deposits
SET tx_signature = $3,
    sweep_last_valid_block_height = $4,
    sweep_submit_count = sweep_submit_count + 1
WHERE id = $1 AND claimed_by = $2 AND status = 'pending' AND tx_signature IS NULL`

	result, err := s.db.ExecContext(ctx, updateSQL, depositID, owner, txSignature, lastValidBlockHeight)
	if err != nil {
		return false, fmt.Errorf("record sweep signature: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record sweep signature rows affected: %w", err)
	}
	return rows == 1, nil
}

// ReplaceSweepSignature swaps the recorded signature for the one the signer actually
// broadcast. The expiry height is cleared because it described the replaced transaction.
func (s *Store) ReplaceSweepSignature(ctx context.Context, depositID, recorded, broadcast string) error {
	if depositID == "" || recorded == "" || broadcast == "" {
		return fmt.Errorf("deposit id and tx signatures are required")
	}

	const updateSQL = `
UPDATE deposits
SET tx_signature = $3, sweep_last_valid_block_height = NULL
WHERE id = $1 AND status = 'pending' AND tx_signature = $2`

	result, err := s.db.ExecContext(ctx, updateSQL, depositID, recorded, broadcast)
	if err != nil {
		return fmt.Errorf("replace sweep signature: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("replace sweep signature rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deposit %s not pending with signature %s", depositID, recorded)
	}
	return nil
}

// SetSweepLastValidBlockHeight bounds a recorded signature whose expiry height is unknown.
func (s *Store) SetSweepLastValidBlockHeight(ctx context.Context, depositID, txSignature string, lastValidBlockHeight int64) error {
	if depositID == "" || txSignature == "" {
		return fmt.Errorf("deposit id and tx signature are required")
	}
	if lastValidBlockHeight < 0 {
		return fmt.Errorf("last valid block height must not be negative")
	}

	const updateSQL = `
UPDATE deposits
SET sweep_last_valid_block_height = $3
WHERE id = $1 AND status = 'pending' AND tx_signature = $2 AND sweep_last_valid_block_height IS NULL`

	if _, err := s.db.ExecContext(ctx, updateSQL, depositID, txSignature, lastValidBlockHeight); err != nil {
		return fmt.Errorf("set sweep last valid block height: %w", err)
	}
	return nil
}

// ClearDroppedSweepSignature forgets a signature that can no longer land so the deposit can be
// swept again. cleared=false when the row moved on (different signature or not pending).
func (s *Store) ClearDroppedSweepSignature(ctx context.Context, depositID, txSignature string) (bool, error) {
	if depositID == "" || txSignature == "" {
		return false, fmt.Errorf("deposit id and tx signature are required")
	}

	const updateSQL = `
UPDATE deposits
SET tx_signature = NULL, sweep_last_valid_block_height = NULL
WHERE id = $1 AND status = 'pending' AND tx_signature = $2`

	result, err := s.db.ExecContext(ctx, updateSQL, depositID, txSignature)
	if err != nil {
		return false, fmt.Errorf("clear dropped sweep signature: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("clear dropped sweep signature rows affected: %w", err)
	}
	return rows == 1, nil
}

// RecordSweepAttemptFailure counts a transient failure and defers the deposit until nextAttemptAt.
func (s *Store) RecordSweepAttemptFailure(ctx context.Context, depositID, lastError string, nextAttemptAt time.Time) error {
	if depositID == "" {
		return fmt.Errorf("deposit id is required")
	}
	lastError = strings.TrimSpace(lastError)
	if len(lastError) > maxDepositLastErrorLen {
		lastError = lastError[:maxDepositLastErrorLen]
	}

	const updateSQL = `
UPDATE deposits
SET attempt_count = attempt_count + 1, next_attempt_at = $3, last_error = $2
WHERE id = $1 AND status = 'pending'`

	if _, err := s.db.ExecContext(ctx, updateSQL, depositID, lastError, nextAttemptAt.UTC()); err != nil {
		return fmt.Errorf("record sweep attempt failure: %w", err)
	}
	return nil
}

// DeferSweepDeposit schedules the next look at a healthy deposit and resets its failure streak.
func (s *Store) DeferSweepDeposit(ctx context.Context, depositID string, nextAttemptAt sql.NullTime) error {
	if depositID == "" {
		return fmt.Errorf("deposit id is required")
	}
	if nextAttemptAt.Valid {
		nextAttemptAt.Time = nextAttemptAt.Time.UTC()
	}

	const updateSQL = `
UPDATE deposits
SET attempt_count = 0, next_attempt_at = $2, last_error = NULL
WHERE id = $1 AND status = 'pending'
  AND (attempt_count <> 0 OR last_error IS NOT NULL OR next_attempt_at IS DISTINCT FROM $2)`

	if _, err := s.db.ExecContext(ctx, updateSQL, depositID, nextAttemptAt); err != nil {
		return fmt.Errorf("defer sweep deposit: %w", err)
	}
	return nil
}

// ClaimTreasuriesForSurplusCheck returns up to limit real groups whose treasury surplus has not
// been checked within every, stamping them so no other tick or instance re-checks them early.
// Faker scale clubs (#153) have dummy treasuries and are never returned.
func (s *Store) ClaimTreasuriesForSurplusCheck(ctx context.Context, now time.Time, every time.Duration, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if every <= 0 {
		return nil, fmt.Errorf("interval must be positive")
	}

	// The due set is materialized: inlined into the UPDATE, the planner may re-run the LIMIT
	// subquery per row and stamp more than limit treasuries.
	const claimSQL = `
WITH due AS MATERIALIZED (
  SELECT t.id
  FROM treasuries t
  JOIN groups g ON g.id = t.group_id
  WHERE NOT g.is_faker
    AND (t.surplus_checked_at IS NULL OR t.surplus_checked_at <= $2)
  ORDER BY t.surplus_checked_at ASC NULLS FIRST, t.created_at ASC
  LIMIT $3
  FOR UPDATE OF t SKIP LOCKED
)
UPDATE treasuries
SET surplus_checked_at = $1
FROM due
WHERE treasuries.id = due.id
RETURNING treasuries.group_id`

	now = now.UTC()
	rows, err := s.db.QueryContext(ctx, claimSQL, now, now.Add(-every), limit)
	if err != nil {
		return nil, fmt.Errorf("claim treasuries for surplus check: %w", err)
	}
	defer rows.Close()

	var groupIDs []string
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scan treasury group id: %w", err)
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate treasury group ids: %w", err)
	}
	return groupIDs, nil
}
