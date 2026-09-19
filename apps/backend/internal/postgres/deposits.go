package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DepositRow is a row in deposits.
type DepositRow struct {
	ID          string
	UserID      string
	GroupID     string
	Amount      int64
	FromAddress string
	Status      string
	TxSignature sql.NullString
	CreatedAt   time.Time
}

// InsertDeposit persists a pending deposit row.
func (s *Store) InsertDeposit(ctx context.Context, userID, groupID string, amount int64, fromAddress string) (DepositRow, error) {
	if userID == "" || groupID == "" {
		return DepositRow{}, fmt.Errorf("user_id and group_id are required")
	}
	if amount <= 0 {
		return DepositRow{}, fmt.Errorf("amount must be positive")
	}
	if fromAddress == "" {
		return DepositRow{}, fmt.Errorf("from_address is required")
	}

	const insertSQL = `
INSERT INTO deposits (user_id, group_id, amount, from_address, status)
VALUES ($1, $2, $3, $4, 'pending')
RETURNING id, user_id, group_id, amount, from_address, status, tx_signature, created_at`

	var row DepositRow
	var txSig sql.NullString
	err := s.db.QueryRowContext(ctx, insertSQL, userID, groupID, amount, fromAddress).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&txSig,
		&row.CreatedAt,
	)
	if err != nil {
		return DepositRow{}, fmt.Errorf("insert deposit: %w", err)
	}
	row.TxSignature = txSig
	return row, nil
}

// GetDepositByID returns a deposit by primary key.
func (s *Store) GetDepositByID(ctx context.Context, id string) (DepositRow, bool, error) {
	if id == "" {
		return DepositRow{}, false, fmt.Errorf("id is required")
	}

	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE id = $1`

	var row DepositRow
	err := s.db.QueryRowContext(ctx, selectSQL, id).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DepositRow{}, false, nil
	}
	if err != nil {
		return DepositRow{}, false, fmt.Errorf("get deposit: %w", err)
	}
	return row, true, nil
}

// ListDepositsByGroupID returns deposits for a group newest first.
func (s *Store) ListDepositsByGroupID(ctx context.Context, groupID string) ([]DepositRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE group_id = $1
ORDER BY created_at DESC
LIMIT 100`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list deposits by group: %w", err)
	}
	defer rows.Close()

	var deposits []DepositRow
	for rows.Next() {
		var row DepositRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.GroupID,
			&row.Amount,
			&row.FromAddress,
			&row.Status,
			&row.TxSignature,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan deposit: %w", err)
		}
		deposits = append(deposits, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deposits: %w", err)
	}
	return deposits, nil
}

// HasPendingDepositsForGroup reports whether the group has any pending deposit rows.
func (s *Store) HasPendingDepositsForGroup(ctx context.Context, groupID string) (bool, error) {
	if groupID == "" {
		return false, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT 1
FROM deposits
WHERE group_id = $1 AND status = 'pending'
LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, selectSQL, groupID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has pending deposits for group: %w", err)
	}
	return true, nil
}

// HasPendingDepositForFromAddress reports whether a pending deposit exists for fromAddress.
func (s *Store) HasPendingDepositForFromAddress(ctx context.Context, fromAddress string) (bool, error) {
	if fromAddress == "" {
		return false, fmt.Errorf("from_address is required")
	}

	const selectSQL = `
SELECT 1
FROM deposits
WHERE from_address = $1 AND status = 'pending'
LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, selectSQL, fromAddress).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has pending deposit for from address: %w", err)
	}
	return true, nil
}

// UpdatePendingDepositAmount sets amount on a pending deposit before sweep broadcast.
func (s *Store) UpdatePendingDepositAmount(ctx context.Context, depositID string, amount int64) error {
	if depositID == "" {
		return fmt.Errorf("deposit id is required")
	}
	if amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}

	const updateSQL = `
UPDATE deposits
SET amount = $2
WHERE id = $1 AND status = 'pending'`

	result, err := s.db.ExecContext(ctx, updateSQL, depositID, amount)
	if err != nil {
		return fmt.Errorf("update pending deposit amount: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update pending deposit amount rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deposit %s not found or not pending", depositID)
	}
	return nil
}

// SumPendingDepositAmountByUserID returns the sum of pending deposit amounts for userID.
func (s *Store) SumPendingDepositAmountByUserID(ctx context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT COALESCE(SUM(amount), 0)
FROM deposits
WHERE user_id = $1 AND status = 'pending'`

	var total int64
	if err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum pending deposit amount: %w", err)
	}
	return total, nil
}

// ListPendingDeposits returns all pending deposit rows.
func (s *Store) ListPendingDeposits(ctx context.Context) ([]DepositRow, error) {
	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE status = 'pending'
ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("list pending deposits: %w", err)
	}
	defer rows.Close()

	var deposits []DepositRow
	for rows.Next() {
		var row DepositRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.GroupID,
			&row.Amount,
			&row.FromAddress,
			&row.Status,
			&row.TxSignature,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan deposit: %w", err)
		}
		deposits = append(deposits, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deposits: %w", err)
	}
	return deposits, nil
}

// FailDeposit marks a pending deposit failed. Returns found=false when the row is missing or not pending.
func (s *Store) FailDeposit(ctx context.Context, depositID, reason string) (DepositRow, bool, error) {
	if depositID == "" {
		return DepositRow{}, false, fmt.Errorf("deposit id is required")
	}

	status := "failed"
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		status = "failed: " + trimmed
	}

	const updateSQL = `
UPDATE deposits
SET status = $2
WHERE id = $1 AND status = 'pending'
RETURNING id, user_id, group_id, amount, from_address, status, tx_signature, created_at`

	var row DepositRow
	err := s.db.QueryRowContext(ctx, updateSQL, depositID, status).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DepositRow{}, false, nil
	}
	if err != nil {
		return DepositRow{}, false, fmt.Errorf("fail deposit: %w", err)
	}
	return row, true, nil
}

// SetDepositBroadcastSignature records a broadcast sweep signature on a pending deposit.
// Status stays pending until ObserveSweep confirms on-chain arrival and credits shares.
func (s *Store) SetDepositBroadcastSignature(ctx context.Context, depositID, txSignature string) error {
	if depositID == "" || txSignature == "" {
		return fmt.Errorf("deposit id and tx signature are required")
	}

	const updateSQL = `
UPDATE deposits
SET tx_signature = $2
WHERE id = $1 AND status = 'pending'`

	result, err := s.db.ExecContext(ctx, updateSQL, depositID, txSignature)
	if err != nil {
		return fmt.Errorf("set deposit broadcast signature: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set deposit broadcast signature rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("deposit %s not found or not pending", depositID)
	}
	return nil
}

// ConfirmDepositTx marks a pending deposit confirmed with a treasury sweep signature.
// Returns newlyConfirmed=false when another caller already confirmed the same deposit.
func (s *Store) ConfirmDepositTx(ctx context.Context, tx *sql.Tx, depositID, txSignature string) (DepositRow, bool, error) {
	if depositID == "" || txSignature == "" {
		return DepositRow{}, false, fmt.Errorf("deposit id and tx signature are required")
	}

	const updateSQL = `
UPDATE deposits
SET status = 'confirmed', tx_signature = $2
WHERE id = $1 AND status = 'pending'
RETURNING id, user_id, group_id, amount, from_address, status, tx_signature, created_at`

	var row DepositRow
	err := tx.QueryRowContext(ctx, updateSQL, depositID, txSignature).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return DepositRow{}, false, fmt.Errorf("confirm deposit: %w", err)
	}

	existing, found, err := getDepositByIDTx(ctx, tx, depositID)
	if err != nil {
		return DepositRow{}, false, err
	}
	if !found {
		return DepositRow{}, false, fmt.Errorf("confirm deposit: deposit not found")
	}
	if existing.Status != "confirmed" {
		return DepositRow{}, false, fmt.Errorf("confirm deposit: deposit not pending")
	}
	if !existing.TxSignature.Valid || existing.TxSignature.String != txSignature {
		return DepositRow{}, false, fmt.Errorf("confirm deposit: tx signature mismatch")
	}
	return existing, false, nil
}

func getDepositByIDTx(ctx context.Context, tx *sql.Tx, id string) (DepositRow, bool, error) {
	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE id = $1`

	var row DepositRow
	err := tx.QueryRowContext(ctx, selectSQL, id).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DepositRow{}, false, nil
	}
	if err != nil {
		return DepositRow{}, false, fmt.Errorf("get deposit: %w", err)
	}
	return row, true, nil
}

// GetDepositByTxSignature returns a confirmed deposit keyed by sweep signature for idempotency.
// Pending deposits may store a broadcast signature before confirmation; those are ignored here.
func (s *Store) GetDepositByTxSignature(ctx context.Context, txSignature string) (DepositRow, bool, error) {
	if txSignature == "" {
		return DepositRow{}, false, fmt.Errorf("tx signature is required")
	}

	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE tx_signature = $1 AND status = 'confirmed'`

	var row DepositRow
	err := s.db.QueryRowContext(ctx, selectSQL, txSignature).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.FromAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DepositRow{}, false, nil
	}
	if err != nil {
		return DepositRow{}, false, fmt.Errorf("get deposit by signature: %w", err)
	}
	return row, true, nil
}
