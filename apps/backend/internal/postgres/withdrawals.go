package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// WithdrawalRow is a row in withdrawals.
type WithdrawalRow struct {
	ID          string
	UserID      string
	GroupID     string
	Amount      int64
	ToAddress   string
	Status      string
	TxSignature sql.NullString
	CreatedAt   time.Time
}

// InsertWithdrawalTx inserts a pending withdrawal row within a transaction.
func (s *Store) InsertWithdrawalTx(ctx context.Context, tx *sql.Tx, userID, groupID string, amount int64, toAddress string) (WithdrawalRow, error) {
	if userID == "" || groupID == "" || toAddress == "" {
		return WithdrawalRow{}, fmt.Errorf("user_id, group_id, and to_address are required")
	}
	if amount <= 0 {
		return WithdrawalRow{}, fmt.Errorf("amount must be positive")
	}

	const insertSQL = `
INSERT INTO withdrawals (user_id, group_id, amount, to_address, status)
VALUES ($1, $2, $3, $4, 'pending')
RETURNING id, user_id, group_id, amount, to_address, status, tx_signature, created_at`

	var row WithdrawalRow
	err := tx.QueryRowContext(ctx, insertSQL, userID, groupID, amount, toAddress).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if err != nil {
		return WithdrawalRow{}, fmt.Errorf("insert withdrawal: %w", err)
	}
	return row, nil
}

// InsertWithdrawal inserts a pending withdrawal row outside a caller transaction.
func (s *Store) InsertWithdrawal(ctx context.Context, userID, groupID string, amount int64, toAddress string) (WithdrawalRow, error) {
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return WithdrawalRow{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	row, err := s.InsertWithdrawalTx(ctx, tx, userID, groupID, amount, toAddress)
	if err != nil {
		return WithdrawalRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return WithdrawalRow{}, fmt.Errorf("commit insert withdrawal: %w", err)
	}
	committed = true
	return row, nil
}

// ConfirmWithdrawalPayoutTx marks a withdrawal settled, increments amount_withdrawn, and writes NAV snapshot once.
func (s *Store) ConfirmWithdrawalPayoutTx(ctx context.Context, tx *sql.Tx, withdrawalID, txSignature string, nav NavSnapshotValues) (WithdrawalRow, bool, error) {
	if withdrawalID == "" || txSignature == "" {
		return WithdrawalRow{}, false, fmt.Errorf("withdrawal id and tx signature are required")
	}

	const updateSQL = `
UPDATE withdrawals
SET status = 'confirmed', tx_signature = $2
WHERE id = $1 AND status = 'pending'
RETURNING id, user_id, group_id, amount, to_address, status, tx_signature, created_at`

	var row WithdrawalRow
	err := tx.QueryRowContext(ctx, updateSQL, withdrawalID, txSignature).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if err == nil {
		if _, err := s.IncrementAmountWithdrawnTx(ctx, tx, row.UserID, row.GroupID, row.Amount); err != nil {
			return WithdrawalRow{}, false, err
		}
		if err := s.WriteNavSnapshotOnWithdrawalPayoutTx(ctx, tx, row.GroupID, nav); err != nil {
			return WithdrawalRow{}, false, err
		}
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRow{}, false, fmt.Errorf("confirm withdrawal payout: %w", err)
	}

	existing, found, err := getWithdrawalByIDTx(ctx, tx, withdrawalID)
	if err != nil {
		return WithdrawalRow{}, false, err
	}
	if !found {
		return WithdrawalRow{}, false, fmt.Errorf("confirm withdrawal payout: withdrawal not found")
	}
	if existing.Status != "confirmed" {
		return WithdrawalRow{}, false, fmt.Errorf("confirm withdrawal payout: withdrawal not pending")
	}
	if !existing.TxSignature.Valid || existing.TxSignature.String != txSignature {
		return WithdrawalRow{}, false, fmt.Errorf("confirm withdrawal payout: tx signature mismatch")
	}
	return existing, false, nil
}

func getWithdrawalByIDTx(ctx context.Context, tx *sql.Tx, id string) (WithdrawalRow, bool, error) {
	const selectSQL = `
SELECT id, user_id, group_id, amount, to_address, status, tx_signature, created_at
FROM withdrawals
WHERE id = $1`

	var row WithdrawalRow
	err := tx.QueryRowContext(ctx, selectSQL, id).Scan(
		&row.ID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxSignature,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WithdrawalRow{}, false, nil
	}
	if err != nil {
		return WithdrawalRow{}, false, fmt.Errorf("get withdrawal: %w", err)
	}
	return row, true, nil
}

// ListWithdrawalsByGroupID returns confirmed and pending withdrawals for a group.
func (s *Store) ListWithdrawalsByGroupID(ctx context.Context, groupID string) ([]WithdrawalRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT id, user_id, group_id, amount, to_address, status, tx_signature, created_at
FROM withdrawals
WHERE group_id = $1
ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list withdrawals by group: %w", err)
	}
	defer rows.Close()

	var out []WithdrawalRow
	for rows.Next() {
		var row WithdrawalRow
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.GroupID,
			&row.Amount,
			&row.ToAddress,
			&row.Status,
			&row.TxSignature,
			&row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan withdrawal: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate withdrawals: %w", err)
	}
	return out, nil
}

// InsertPayoutProofTx records a verified payout proof linked to a withdrawal.
func (s *Store) InsertPayoutProofTx(ctx context.Context, tx *sql.Tx, userID, groupID, payoutAddress, message, signature, withdrawalID string) error {
	if userID == "" || groupID == "" || payoutAddress == "" || message == "" || signature == "" {
		return fmt.Errorf("payout proof fields are required")
	}

	const insertSQL = `
INSERT INTO payout_proofs (user_id, group_id, payout_address, message, signature, withdrawal_id)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid)`

	_, err := tx.ExecContext(ctx, insertSQL, userID, groupID, payoutAddress, message, signature, withdrawalID)
	if err != nil {
		return fmt.Errorf("insert payout proof: %w", err)
	}
	return nil
}
