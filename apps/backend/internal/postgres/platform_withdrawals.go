package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PlatformWithdrawalRow is a row in platform_withdrawals.
type PlatformWithdrawalRow struct {
	ID        string
	UserID    string
	Amount    int64
	ToAddress string
	Status    string
	TxHash    sql.NullString
	CreatedAt time.Time
}

// InsertPlatformWithdrawal inserts a pending platform withdrawal row.
func (s *Store) InsertPlatformWithdrawal(ctx context.Context, userID string, amount int64, toAddress string) (PlatformWithdrawalRow, error) {
	if userID == "" || toAddress == "" {
		return PlatformWithdrawalRow{}, fmt.Errorf("user_id and to_address are required")
	}
	if amount <= 0 {
		return PlatformWithdrawalRow{}, fmt.Errorf("amount must be positive")
	}

	const insertSQL = `
INSERT INTO platform_withdrawals (user_id, amount, to_address, status)
VALUES ($1, $2, $3, 'pending')
RETURNING id, user_id, amount, to_address, status, tx_hash, created_at`

	var row PlatformWithdrawalRow
	err := s.db.QueryRowContext(ctx, insertSQL, userID, amount, toAddress).Scan(
		&row.ID,
		&row.UserID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxHash,
		&row.CreatedAt,
	)
	if err != nil {
		return PlatformWithdrawalRow{}, fmt.Errorf("insert platform withdrawal: %w", err)
	}
	return row, nil
}

// HasPendingPlatformWithdrawalForUser reports whether userID has an in-flight platform withdrawal.
func (s *Store) HasPendingPlatformWithdrawalForUser(ctx context.Context, userID string) (bool, error) {
	if userID == "" {
		return false, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT 1
FROM platform_withdrawals
WHERE user_id = $1 AND status = 'pending'
LIMIT 1`

	var exists int
	err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has pending platform withdrawal: %w", err)
	}
	return true, nil
}

// SumPendingPlatformWithdrawalAmountByUserID returns the sum of pending platform withdrawal amounts.
func (s *Store) SumPendingPlatformWithdrawalAmountByUserID(ctx context.Context, userID string) (int64, error) {
	if userID == "" {
		return 0, fmt.Errorf("user_id is required")
	}

	const selectSQL = `
SELECT COALESCE(SUM(amount), 0)
FROM platform_withdrawals
WHERE user_id = $1 AND status = 'pending'`

	var total int64
	if err := s.db.QueryRowContext(ctx, selectSQL, userID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum pending platform withdrawal amount: %w", err)
	}
	return total, nil
}

// GetPlatformWithdrawalByID returns a platform withdrawal by primary key.
func (s *Store) GetPlatformWithdrawalByID(ctx context.Context, id string) (PlatformWithdrawalRow, bool, error) {
	if id == "" {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("id is required")
	}

	const selectSQL = `
SELECT id, user_id, amount, to_address, status, tx_hash, created_at
FROM platform_withdrawals
WHERE id = $1`

	var row PlatformWithdrawalRow
	err := s.db.QueryRowContext(ctx, selectSQL, id).Scan(
		&row.ID,
		&row.UserID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxHash,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformWithdrawalRow{}, false, nil
	}
	if err != nil {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("get platform withdrawal: %w", err)
	}
	return row, true, nil
}

// GetPlatformWithdrawalByTxHash returns a platform withdrawal keyed by on-chain signature.
func (s *Store) GetPlatformWithdrawalByTxHash(ctx context.Context, txHash string) (PlatformWithdrawalRow, bool, error) {
	if txHash == "" {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("tx signature is required")
	}

	const selectSQL = `
SELECT id, user_id, amount, to_address, status, tx_hash, created_at
FROM platform_withdrawals
WHERE tx_hash = $1`

	var row PlatformWithdrawalRow
	err := s.db.QueryRowContext(ctx, selectSQL, txHash).Scan(
		&row.ID,
		&row.UserID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxHash,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformWithdrawalRow{}, false, nil
	}
	if err != nil {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("get platform withdrawal by signature: %w", err)
	}
	return row, true, nil
}

type platformWithdrawalTestHooks struct {
	failSetBroadcastSignature bool
	setBroadcastSignatureErr  error
}

// SetFailPlatformWithdrawalBroadcastSignatureForTests forces the next SetPlatformWithdrawalBroadcastSignature call to fail.
func (s *Store) SetFailPlatformWithdrawalBroadcastSignatureForTests(fail bool, err error) {
	s.platformWithdrawalTestHooks.failSetBroadcastSignature = fail
	s.platformWithdrawalTestHooks.setBroadcastSignatureErr = err
}

// SetPlatformWithdrawalBroadcastSignature records the broadcast signature on a pending row.
func (s *Store) SetPlatformWithdrawalBroadcastSignature(ctx context.Context, withdrawalID, txHash string) error {
	if withdrawalID == "" || txHash == "" {
		return fmt.Errorf("withdrawal id and tx signature are required")
	}
	if s.platformWithdrawalTestHooks.failSetBroadcastSignature {
		err := s.platformWithdrawalTestHooks.setBroadcastSignatureErr
		if err == nil {
			err = fmt.Errorf("injected set platform withdrawal broadcast signature error")
		}
		return err
	}

	const updateSQL = `
UPDATE platform_withdrawals
SET tx_hash = $2
WHERE id = $1 AND status = 'pending'`

	result, err := s.db.ExecContext(ctx, updateSQL, withdrawalID, txHash)
	if err != nil {
		return fmt.Errorf("set platform withdrawal broadcast signature: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set platform withdrawal broadcast signature rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("platform withdrawal %s not found or not pending", withdrawalID)
	}
	return nil
}

// ConfirmPlatformWithdrawal marks a pending platform withdrawal confirmed with its signature.
func (s *Store) ConfirmPlatformWithdrawal(ctx context.Context, withdrawalID, txHash string) (PlatformWithdrawalRow, bool, error) {
	if withdrawalID == "" || txHash == "" {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("withdrawal id and tx signature are required")
	}

	const updateSQL = `
UPDATE platform_withdrawals
SET status = 'confirmed', tx_hash = $2
WHERE id = $1 AND status = 'pending'
RETURNING id, user_id, amount, to_address, status, tx_hash, created_at`

	var row PlatformWithdrawalRow
	err := s.db.QueryRowContext(ctx, updateSQL, withdrawalID, txHash).Scan(
		&row.ID,
		&row.UserID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxHash,
		&row.CreatedAt,
	)
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("confirm platform withdrawal: %w", err)
	}

	existing, found, err := s.GetPlatformWithdrawalByID(ctx, withdrawalID)
	if err != nil {
		return PlatformWithdrawalRow{}, false, err
	}
	if !found {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("confirm platform withdrawal: withdrawal not found")
	}
	if existing.Status != "confirmed" {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("confirm platform withdrawal: withdrawal not pending")
	}
	if !existing.TxHash.Valid || existing.TxHash.String != txHash {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("confirm platform withdrawal: tx signature mismatch")
	}
	return existing, false, nil
}

// FailPlatformWithdrawal marks a pending platform withdrawal failed.
func (s *Store) FailPlatformWithdrawal(ctx context.Context, withdrawalID, reason string) (PlatformWithdrawalRow, bool, error) {
	if withdrawalID == "" {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("withdrawal id is required")
	}

	status := "failed"
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		status = "failed: " + trimmed
	}

	const updateSQL = `
UPDATE platform_withdrawals
SET status = $2
WHERE id = $1 AND status = 'pending'
RETURNING id, user_id, amount, to_address, status, tx_hash, created_at`

	var row PlatformWithdrawalRow
	err := s.db.QueryRowContext(ctx, updateSQL, withdrawalID, status).Scan(
		&row.ID,
		&row.UserID,
		&row.Amount,
		&row.ToAddress,
		&row.Status,
		&row.TxHash,
		&row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformWithdrawalRow{}, false, nil
	}
	if err != nil {
		return PlatformWithdrawalRow{}, false, fmt.Errorf("fail platform withdrawal: %w", err)
	}
	return row, true, nil
}
