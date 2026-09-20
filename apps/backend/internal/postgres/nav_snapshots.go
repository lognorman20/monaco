package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/packages/domain"
)


type navSnapshotQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// NavSnapshotReason records why a NAV snapshot was written.
type NavSnapshotReason string

const (
	NavSnapshotReasonDeposit            NavSnapshotReason = "deposit"
	NavSnapshotReasonTransactionConfirm NavSnapshotReason = "transaction_confirm"
	NavSnapshotReasonWithdrawalPayout   NavSnapshotReason = "withdrawal_payout"
)

// NavSnapshotRow is a row in nav_snapshots.
type NavSnapshotRow struct {
	ID                string
	GroupID           string
	PotNavMicros      int64
	NavPerShareMicros int64
	TotalShares       int64
	// NetContributedMicros is the group's net USDC in (deposits credited minus
	// payouts) as of this snapshot, recorded in the same transaction. Nil for
	// rows written before migration 000012.
	NetContributedMicros *int64
	Reason               NavSnapshotReason
	CreatedAt            time.Time
}

const navSnapshotColumns = `id, group_id, pot_nav_micros, nav_per_share_micros, total_shares, net_contributed_micros, reason, created_at`

type navSnapshotScanner interface {
	Scan(dest ...any) error
}

func scanNavSnapshot(row navSnapshotScanner) (NavSnapshotRow, error) {
	var out NavSnapshotRow
	var reason string
	var netContributed sql.NullInt64
	if err := row.Scan(
		&out.ID,
		&out.GroupID,
		&out.PotNavMicros,
		&out.NavPerShareMicros,
		&out.TotalShares,
		&netContributed,
		&reason,
		&out.CreatedAt,
	); err != nil {
		return NavSnapshotRow{}, err
	}
	if netContributed.Valid {
		v := netContributed.Int64
		out.NetContributedMicros = &v
	}
	out.Reason = NavSnapshotReason(reason)
	out.CreatedAt = out.CreatedAt.UTC()
	return out, nil
}

// NavSnapshotValues is the computed NAV state persisted to nav_snapshots.
type NavSnapshotValues struct {
	PotNavMicros      int64
	NavPerShareMicros int64
	TotalShares       int64
}

// InsertNavSnapshotTx writes a snapshot within an existing transaction.
func (s *Store) InsertNavSnapshotTx(ctx context.Context, tx *sql.Tx, groupID string, reason NavSnapshotReason, vals NavSnapshotValues) (NavSnapshotRow, error) {
	if groupID == "" {
		return NavSnapshotRow{}, fmt.Errorf("group_id is required")
	}
	if vals.PotNavMicros < 0 || vals.NavPerShareMicros < 0 || vals.TotalShares < 0 {
		return NavSnapshotRow{}, fmt.Errorf("nav snapshot values must be non-negative")
	}
	switch reason {
	case NavSnapshotReasonDeposit, NavSnapshotReasonTransactionConfirm, NavSnapshotReasonWithdrawalPayout:
	default:
		return NavSnapshotRow{}, fmt.Errorf("invalid nav snapshot reason %q", reason)
	}

	// net_contributed_micros is read inside the same transaction as the
	// position change that triggered this snapshot, so it pairs exactly with
	// pot_nav_micros: group P&L at this instant = pot - net contributed.
	// Only pot-backed positions count (ghost faker positions in a real group
	// never funded the pot), matching the live net-in the P&L series ends on.
	const insertSQL = `
INSERT INTO nav_snapshots (group_id, pot_nav_micros, nav_per_share_micros, total_shares, reason, net_contributed_micros)
VALUES ($1, $2, $3, $4, $5, (
  SELECT COALESCE(SUM(p.amount_deposited - p.amount_withdrawn), 0)
  FROM positions p
  JOIN users u ON u.id = p.user_id
  JOIN groups g ON g.id = p.group_id
  WHERE p.group_id = $1 AND ` + potPositionPredicate + `
))
RETURNING ` + navSnapshotColumns

	row, err := scanNavSnapshot(tx.QueryRowContext(ctx, insertSQL, groupID, vals.PotNavMicros, vals.NavPerShareMicros, vals.TotalShares, string(reason)))
	if err != nil {
		return NavSnapshotRow{}, fmt.Errorf("insert nav snapshot: %w", err)
	}
	return row, nil
}

// WriteNavSnapshot persists a snapshot outside an caller-managed transaction.
func (s *Store) WriteNavSnapshot(ctx context.Context, groupID string, reason NavSnapshotReason, vals NavSnapshotValues) (NavSnapshotRow, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return NavSnapshotRow{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	row, err := s.InsertNavSnapshotTx(ctx, tx, groupID, reason, vals)
	if err != nil {
		return NavSnapshotRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return NavSnapshotRow{}, fmt.Errorf("commit nav snapshot: %w", err)
	}
	committed = true
	return row, nil
}

// WriteNavSnapshotOnDepositConfirmTx records pot NAV after a confirmed deposit credit.
// The store only persists: vals come from the app's single pot valuation.
func (s *Store) WriteNavSnapshotOnDepositConfirmTx(ctx context.Context, tx *sql.Tx, groupID string, vals NavSnapshotValues) error {
	_, err := s.InsertNavSnapshotTx(ctx, tx, groupID, NavSnapshotReasonDeposit, vals)
	return err
}

// WriteNavSnapshotOnTransactionConfirm records pot NAV after a newly confirmed transaction.
func (s *Store) WriteNavSnapshotOnTransactionConfirm(ctx context.Context, groupID string, vals NavSnapshotValues) error {
	_, err := s.WriteNavSnapshot(ctx, groupID, NavSnapshotReasonTransactionConfirm, vals)
	return err
}

// RecordTreasuryHoldingsAndNavSnapshotOnConfirm verifies confirmed buy holdings and writes pot NAV.
func (s *Store) RecordTreasuryHoldingsAndNavSnapshotOnConfirm(
	ctx context.Context,
	groupID string,
	tx TransactionRow,
	vals NavSnapshotValues,
) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}
	if tx.Status != TransactionStatusConfirmed || tx.Action != TransactionActionBuy {
		return fmt.Errorf("transaction must be a confirmed buy")
	}
	if tx.OutputMint == "" {
		return fmt.Errorf("output mint is required")
	}

	holdings, err := s.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if !holdingIncludesMint(holdings, tx.OutputMint) {
		return fmt.Errorf("treasury holdings missing output mint %s", tx.OutputMint)
	}

	return s.WriteNavSnapshotOnTransactionConfirm(ctx, groupID, vals)
}

func holdingIncludesMint(holdings []TokenHoldingRow, mint string) bool {
	for _, holding := range holdings {
		if holding.Mint == mint && holding.Amount > 0 {
			return true
		}
	}
	return false
}

// WriteNavSnapshotOnWithdrawalPayoutTx records pot NAV after a confirmed withdrawal payout.
func (s *Store) WriteNavSnapshotOnWithdrawalPayoutTx(ctx context.Context, tx *sql.Tx, groupID string, vals NavSnapshotValues) error {
	_, err := s.InsertNavSnapshotTx(ctx, tx, groupID, NavSnapshotReasonWithdrawalPayout, vals)
	return err
}

// SumShareUnitsByGroupTx returns total share_units within tx.
func (s *Store) SumShareUnitsByGroupTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	return sumShareUnitsByGroupQuery(ctx, tx, groupID)
}

// ListNetTokenHoldingsByGroupTx aggregates holdings visible within tx.
func (s *Store) ListNetTokenHoldingsByGroupTx(ctx context.Context, tx *sql.Tx, groupID string) ([]TokenHoldingRow, error) {
	return listNetTokenHoldingsByGroupQuery(ctx, tx, groupID)
}

// GetFillDerivedCostBasisByOutputMintTx returns fill cost basis visible within tx.
func (s *Store) GetFillDerivedCostBasisByOutputMintTx(ctx context.Context, tx *sql.Tx, groupID, outputMint string) (int64, int64, bool, error) {
	return fillDerivedCostBasisByOutputMintQuery(ctx, tx, groupID, outputMint)
}

func sumShareUnitsByGroupQuery(ctx context.Context, q navSnapshotQuerier, groupID string) (int64, error) {
	const selectSQL = `
SELECT COALESCE(SUM(p.share_units), 0)
FROM positions p
JOIN users u ON u.id = p.user_id
JOIN groups g ON g.id = p.group_id
WHERE p.group_id = $1 AND ` + potPositionPredicate
	var total int64
	if err := q.QueryRowContext(ctx, selectSQL, groupID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum share units: %w", err)
	}
	return total, nil
}

func listNetTokenHoldingsByGroupQuery(ctx context.Context, q navSnapshotQuerier, groupID string) ([]TokenHoldingRow, error) {
	const selectSQL = `
WITH buys AS (
  SELECT output_mint AS mint, COALESCE(SUM(cost_basis_amount), 0) AS amount
  FROM transactions
  WHERE group_id = $1 AND action = 'buy' AND status = 'confirmed'
  GROUP BY output_mint
),
sells AS (
  SELECT input_mint AS mint, COALESCE(SUM(amount), 0) AS amount
  FROM transactions
  WHERE group_id = $1 AND action = 'sell' AND status = 'confirmed'
  GROUP BY input_mint
)
SELECT COALESCE(buys.mint, sells.mint) AS mint,
       COALESCE(buys.amount, 0) - COALESCE(sells.amount, 0) AS amount
FROM buys
FULL OUTER JOIN sells ON buys.mint = sells.mint
WHERE COALESCE(buys.amount, 0) - COALESCE(sells.amount, 0) > 0
ORDER BY mint`

	rows, err := q.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list net token holdings: %w", err)
	}
	defer rows.Close()

	var holdings []TokenHoldingRow
	for rows.Next() {
		var row TokenHoldingRow
		if err := rows.Scan(&row.Mint, &row.Amount); err != nil {
			return nil, fmt.Errorf("scan token holding: %w", err)
		}
		holdings = append(holdings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token holdings: %w", err)
	}
	return holdings, nil
}

func fillDerivedCostBasisByOutputMintQuery(ctx context.Context, q navSnapshotQuerier, groupID, outputMint string) (int64, int64, bool, error) {
	const selectSQL = `
WITH buys AS (
  SELECT COALESCE(SUM(cost_basis_price), 0)  AS usdc,
         COALESCE(SUM(cost_basis_amount), 0) AS tokens
  FROM transactions
  WHERE group_id = $1 AND output_mint = $2
    AND action = 'buy' AND status = 'confirmed'
),
sells AS (
  SELECT COALESCE(SUM(amount), 0) AS tokens
  FROM transactions
  WHERE group_id = $1 AND input_mint = $2
    AND action = 'sell' AND status = 'confirmed'
)
SELECT buys.usdc, buys.tokens, sells.tokens FROM buys, sells`

	var buyUSDC, buyTokens, sellTokens int64
	err := q.QueryRowContext(ctx, selectSQL, groupID, outputMint).Scan(&buyUSDC, &buyTokens, &sellTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("get fill-derived cost basis: %w", err)
	}
	if buyTokens <= 0 {
		return 0, 0, false, nil
	}
	remaining := buyTokens - sellTokens
	if remaining <= 0 {
		return 0, 0, false, nil
	}
	remainingBasis, err := domain.MulDivFloor(buyUSDC, remaining, buyTokens)
	if err != nil {
		return 0, 0, false, fmt.Errorf("remaining cost basis: %w", err)
	}
	return remainingBasis, remaining, true, nil
}

// ListNavSnapshotsForGroupsSince returns snapshots for groupIDs at or after since, oldest first.
func (s *Store) ListNavSnapshotsForGroupsSince(ctx context.Context, groupIDs []string, since time.Time) ([]NavSnapshotRow, error) {
	if len(groupIDs) == 0 {
		return []NavSnapshotRow{}, nil
	}

	selectSQL := `
SELECT ` + navSnapshotColumns + `
FROM nav_snapshots
WHERE group_id = ANY($1::uuid[]) AND created_at >= $2
ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupIDs, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("list nav snapshots for groups since: %w", err)
	}
	defer rows.Close()

	var out []NavSnapshotRow
	for rows.Next() {
		row, err := scanNavSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan nav snapshot: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nav snapshots for groups since: %w", err)
	}
	return out, nil
}

// GetNavSnapshotAtOrBefore returns the latest snapshot at or before at for groupID.
func (s *Store) GetNavSnapshotAtOrBefore(ctx context.Context, groupID string, at time.Time) (NavSnapshotRow, bool, error) {
	if groupID == "" {
		return NavSnapshotRow{}, false, fmt.Errorf("group_id is required")
	}

	selectSQL := `
SELECT ` + navSnapshotColumns + `
FROM nav_snapshots
WHERE group_id = $1 AND created_at <= $2
ORDER BY created_at DESC, id DESC
LIMIT 1`

	row, err := scanNavSnapshot(s.db.QueryRowContext(ctx, selectSQL, groupID, at.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return NavSnapshotRow{}, false, nil
	}
	if err != nil {
		return NavSnapshotRow{}, false, fmt.Errorf("get nav snapshot at or before: %w", err)
	}
	return row, true, nil
}

// ListNavSnapshotsByGroup returns snapshots newest first.
func (s *Store) ListNavSnapshotsByGroup(ctx context.Context, groupID string) ([]NavSnapshotRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	selectSQL := `
SELECT ` + navSnapshotColumns + `
FROM nav_snapshots
WHERE group_id = $1
ORDER BY created_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list nav snapshots: %w", err)
	}
	defer rows.Close()

	var out []NavSnapshotRow
	for rows.Next() {
		row, err := scanNavSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan nav snapshot: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nav snapshots: %w", err)
	}
	return out, nil
}

// CountNavSnapshotsByGroupAndReason counts persisted snapshots for assertions.
func (s *Store) CountNavSnapshotsByGroupAndReason(ctx context.Context, groupID string, reason NavSnapshotReason) (int, error) {
	if groupID == "" {
		return 0, fmt.Errorf("group_id is required")
	}

	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM nav_snapshots WHERE group_id = $1 AND reason = $2`, groupID, string(reason)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count nav snapshots: %w", err)
	}
	return count, nil
}
