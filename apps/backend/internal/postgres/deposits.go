package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// ConfirmDepositTx marks a deposit confirmed with a treasury sweep signature.
func (s *Store) ConfirmDepositTx(ctx context.Context, tx *sql.Tx, depositID, txSignature string) (DepositRow, error) {
	if depositID == "" || txSignature == "" {
		return DepositRow{}, fmt.Errorf("deposit id and tx signature are required")
	}

	const updateSQL = `
UPDATE deposits
SET status = 'confirmed', tx_signature = $2
WHERE id = $1
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
	if err != nil {
		return DepositRow{}, fmt.Errorf("confirm deposit: %w", err)
	}
	return row, nil
}

// GetDepositByTxSignature returns a deposit keyed by sweep signature for idempotency.
func (s *Store) GetDepositByTxSignature(ctx context.Context, txSignature string) (DepositRow, bool, error) {
	if txSignature == "" {
		return DepositRow{}, false, fmt.Errorf("tx signature is required")
	}

	const selectSQL = `
SELECT id, user_id, group_id, amount, from_address, status, tx_signature, created_at
FROM deposits
WHERE tx_signature = $1`

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
