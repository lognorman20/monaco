package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

// memberFundsLockNamespace separates member funds locks from other two-key advisory locks.
const memberFundsLockNamespace = "member_funds"

// LockMemberFundsTx serializes platform-balance reservations for one member until tx ends.
// The balance check and the reserving insert must both happen on tx, otherwise two
// concurrent fund requests each see the other's reservation missing and over-reserve.
func (s *Store) LockMemberFundsTx(ctx context.Context, tx *sql.Tx, userID string) error {
	if userID == "" {
		return fmt.Errorf("user_id is required")
	}
	if _, err := tx.ExecContext(ctx, `
SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`,
		memberFundsLockNamespace, userID,
	); err != nil {
		return fmt.Errorf("lock member funds: %w", err)
	}
	return nil
}

// SumPendingReservationsByUserIDTx returns in-flight fund-to-cabal intents plus pending
// platform withdrawals for userID, read inside the member funds lock.
func (s *Store) SumPendingReservationsByUserIDTx(ctx context.Context, tx *sql.Tx, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT
  (SELECT COALESCE(SUM(amount), 0) FROM deposits WHERE user_id = $1 AND status = 'pending') +
  (SELECT COALESCE(SUM(amount), 0) FROM platform_withdrawals WHERE user_id = $1 AND status = 'pending')`

	var total int64
	if err := tx.QueryRowContext(ctx, selectSQL, userID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum pending reservations: %w", err)
	}
	return total, nil
}

// InsertDepositTx persists a pending deposit row inside tx.
func (s *Store) InsertDepositTx(ctx context.Context, tx *sql.Tx, userID, groupID string, amount int64, fromAddress string) (DepositRow, error) {
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
	err := tx.QueryRowContext(ctx, insertSQL, userID, groupID, amount, fromAddress).Scan(
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
		return DepositRow{}, fmt.Errorf("insert deposit: %w", err)
	}
	return row, nil
}
