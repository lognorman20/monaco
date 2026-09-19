package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// RedeemJobRow is a row in redeem_jobs.
type RedeemJobRow struct {
	ID             string
	GroupID        string
	UserID         string
	ShareUnits     int64
	SliceUsdc      int64
	PayoutAddress  string
	Status         string
	WithdrawalID   sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

const (
	RedeemJobStatusDebited = "debited"
	RedeemJobStatusSelling = "selling"
	RedeemJobStatusPaying  = "paying"
	RedeemJobStatusSettled = "settled"
)

// InsertRedeemJobTx inserts a debited redeem job within a transaction.
func (s *Store) InsertRedeemJobTx(ctx context.Context, tx *sql.Tx, userID, groupID string, shareUnits, sliceUsdc int64, payoutAddress string) (RedeemJobRow, error) {
	if userID == "" || groupID == "" || payoutAddress == "" {
		return RedeemJobRow{}, fmt.Errorf("user_id, group_id, and payout_address are required")
	}
	if shareUnits <= 0 || sliceUsdc <= 0 {
		return RedeemJobRow{}, fmt.Errorf("share units and slice usdc must be positive")
	}

	const insertSQL = `
INSERT INTO redeem_jobs (group_id, user_id, share_units, slice_usdc, payout_address, status)
VALUES ($1, $2, $3, $4, $5, 'debited')
RETURNING id, group_id, user_id, share_units, slice_usdc, payout_address, status, withdrawal_id, created_at, updated_at`

	var row RedeemJobRow
	var withdrawalID sql.NullString
	err := tx.QueryRowContext(ctx, insertSQL, groupID, userID, shareUnits, sliceUsdc, payoutAddress).Scan(
		&row.ID,
		&row.GroupID,
		&row.UserID,
		&row.ShareUnits,
		&row.SliceUsdc,
		&row.PayoutAddress,
		&row.Status,
		&withdrawalID,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return RedeemJobRow{}, ErrActiveRedeemJobExists
		}
		return RedeemJobRow{}, fmt.Errorf("insert redeem job: %w", err)
	}
	row.WithdrawalID = withdrawalID
	return row, nil
}

// ErrActiveRedeemJobExists means the member already has an in-flight redeem job.
var ErrActiveRedeemJobExists = errors.New("active redeem job exists")

// IsUniqueViolation reports Postgres unique-constraint failures.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// HasActiveRedeemJobForUserTx reports unsettled redeem jobs within tx.
func (s *Store) HasActiveRedeemJobForUserTx(ctx context.Context, tx *sql.Tx, userID, groupID string) (bool, error) {
	if userID == "" || groupID == "" {
		return false, fmt.Errorf("user_id and group_id are required")
	}
	const selectSQL = `SELECT 1 FROM redeem_jobs WHERE user_id = $1 AND group_id = $2 AND status <> 'settled' LIMIT 1`
	var exists int
	err := tx.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has active redeem job tx: %w", err)
	}
	return true, nil
}

// GetActiveRedeemJobForUser returns the member's in-flight redeem job in groupID, if any.
func (s *Store) GetActiveRedeemJobForUser(ctx context.Context, userID, groupID string) (RedeemJobRow, bool, error) {
	if userID == "" || groupID == "" {
		return RedeemJobRow{}, false, fmt.Errorf("user_id and group_id are required")
	}
	const selectSQL = `
SELECT id, group_id, user_id, share_units, slice_usdc, payout_address, status, withdrawal_id, created_at, updated_at
FROM redeem_jobs
WHERE user_id = $1 AND group_id = $2 AND status <> 'settled'
ORDER BY created_at DESC
LIMIT 1`

	var row RedeemJobRow
	err := s.db.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(
		&row.ID,
		&row.GroupID,
		&row.UserID,
		&row.ShareUnits,
		&row.SliceUsdc,
		&row.PayoutAddress,
		&row.Status,
		&row.WithdrawalID,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RedeemJobRow{}, false, nil
	}
	if err != nil {
		return RedeemJobRow{}, false, fmt.Errorf("get active redeem job: %w", err)
	}
	return row, true, nil
}

// DeleteRedeemJobTx removes a redeem job within a transaction.
func (s *Store) DeleteRedeemJobTx(ctx context.Context, tx *sql.Tx, jobID string) error {
	if jobID == "" {
		return fmt.Errorf("job id is required")
	}
	const deleteSQL = `DELETE FROM redeem_jobs WHERE id = $1`
	result, err := tx.ExecContext(ctx, deleteSQL, jobID)
	if err != nil {
		return fmt.Errorf("delete redeem job: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete redeem job rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("redeem job not found")
	}
	return nil
}

// HasActiveRedeemJobForUser reports whether userID has an unsettled redeem job in groupID.
func (s *Store) HasActiveRedeemJobForUser(ctx context.Context, userID, groupID string) (bool, error) {
	if userID == "" || groupID == "" {
		return false, fmt.Errorf("user_id and group_id are required")
	}
	const selectSQL = `SELECT 1 FROM redeem_jobs WHERE user_id = $1 AND group_id = $2 AND status <> 'settled' LIMIT 1`
	var exists int
	err := s.db.QueryRowContext(ctx, selectSQL, userID, groupID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has active redeem job: %w", err)
	}
	return true, nil
}

// GetRedeemJobByID returns a redeem job by id.
func (s *Store) GetRedeemJobByID(ctx context.Context, jobID string) (RedeemJobRow, bool, error) {
	if jobID == "" {
		return RedeemJobRow{}, false, fmt.Errorf("job id is required")
	}

	const selectSQL = `
SELECT id, group_id, user_id, share_units, slice_usdc, payout_address, status, withdrawal_id, created_at, updated_at
FROM redeem_jobs
WHERE id = $1`

	var row RedeemJobRow
	err := s.db.QueryRowContext(ctx, selectSQL, jobID).Scan(
		&row.ID,
		&row.GroupID,
		&row.UserID,
		&row.ShareUnits,
		&row.SliceUsdc,
		&row.PayoutAddress,
		&row.Status,
		&row.WithdrawalID,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RedeemJobRow{}, false, nil
	}
	if err != nil {
		return RedeemJobRow{}, false, fmt.Errorf("get redeem job: %w", err)
	}
	return row, true, nil
}

// UpdateRedeemJobStatus updates redeem_jobs.status and updated_at.
func (s *Store) UpdateRedeemJobStatus(ctx context.Context, jobID, status string) error {
	if jobID == "" || status == "" {
		return fmt.Errorf("job id and status are required")
	}

	const updateSQL = `
UPDATE redeem_jobs
SET status = $2, updated_at = now()
WHERE id = $1`

	result, err := s.db.ExecContext(ctx, updateSQL, jobID, status)
	if err != nil {
		return fmt.Errorf("update redeem job status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update redeem job status rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("redeem job not found")
	}
	return nil
}

// AttachWithdrawalToRedeemJobTx links a withdrawal row after payout insert.
func (s *Store) AttachWithdrawalToRedeemJobTx(ctx context.Context, tx *sql.Tx, jobID, withdrawalID string) error {
	if jobID == "" || withdrawalID == "" {
		return fmt.Errorf("job id and withdrawal id are required")
	}

	const updateSQL = `
UPDATE redeem_jobs
SET withdrawal_id = $2, status = 'settled', updated_at = now()
WHERE id = $1`

	result, err := tx.ExecContext(ctx, updateSQL, jobID, withdrawalID)
	if err != nil {
		return fmt.Errorf("attach withdrawal to redeem job: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("attach withdrawal rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("redeem job not found")
	}
	return nil
}
