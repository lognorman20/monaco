package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// RedeemPayoutRow is a row in redeem_payouts: the one treasury transfer signed for a redeem job.
type RedeemPayoutRow struct {
	ID                   string
	RedeemJobID          string
	UserID               string
	GroupID              string
	Amount               int64
	ToAddress            string
	TxSignature          string
	SignedTx             string
	LastValidBlockHeight uint64
	ProofMessage         sql.NullString
	ProofSignature       sql.NullString
	Status               string
	FailureReason        sql.NullString
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

const (
	RedeemPayoutStatusPending   = "pending"
	RedeemPayoutStatusConfirmed = "confirmed"
	RedeemPayoutStatusDropped   = "dropped"
	RedeemPayoutStatusFailed    = "failed"
)

// ErrRedeemPayoutExists means the redeem job already has a payout attempt on record.
var ErrRedeemPayoutExists = errors.New("redeem payout already recorded")

// RedeemPayoutIntent is the signed transfer to record before it is broadcast.
type RedeemPayoutIntent struct {
	RedeemJobID          string
	UserID               string
	GroupID              string
	Amount               int64
	ToAddress            string
	TxSignature          string
	SignedTx             string
	LastValidBlockHeight uint64
	ProofMessage         string
	ProofSignature       string
}

const redeemPayoutColumns = `id, redeem_job_id, user_id, group_id, amount, to_address, tx_signature, signed_tx,
  last_valid_block_height, proof_message, proof_signature, status, failure_reason, created_at, updated_at`

// A block height is a uint64 on chain and a bigint in Postgres. Both directions are checked:
// a wrapped height would make a live transfer look expired, or an expired one look live.
func blockHeightToBigint(height uint64) (int64, error) {
	if height == 0 || height > math.MaxInt64 {
		return 0, fmt.Errorf("last valid block height %d is out of range", height)
	}
	return int64(height), nil
}

func scanRedeemPayout(scan func(dest ...any) error) (RedeemPayoutRow, error) {
	var row RedeemPayoutRow
	var lastValidBlockHeight int64
	err := scan(
		&row.ID,
		&row.RedeemJobID,
		&row.UserID,
		&row.GroupID,
		&row.Amount,
		&row.ToAddress,
		&row.TxSignature,
		&row.SignedTx,
		&lastValidBlockHeight,
		&row.ProofMessage,
		&row.ProofSignature,
		&row.Status,
		&row.FailureReason,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return RedeemPayoutRow{}, err
	}
	if lastValidBlockHeight <= 0 {
		return RedeemPayoutRow{}, fmt.Errorf("redeem payout %s: stored last valid block height %d is out of range", row.ID, lastValidBlockHeight)
	}
	row.LastValidBlockHeight = uint64(lastValidBlockHeight)
	return row, nil
}

// RecordRedeemPayoutIntent writes the signed transfer and moves the job to `paying` in one
// transaction. It must commit before the transfer is broadcast: from then on a `paying` job
// always names the signature whose fate decides it. A job gets one attempt, so a second
// intent for the same job returns ErrRedeemPayoutExists.
func (s *Store) RecordRedeemPayoutIntent(ctx context.Context, intent RedeemPayoutIntent) (RedeemPayoutRow, error) {
	if intent.RedeemJobID == "" || intent.UserID == "" || intent.GroupID == "" || intent.ToAddress == "" {
		return RedeemPayoutRow{}, fmt.Errorf("redeem job id, user_id, group_id, and to_address are required")
	}
	if intent.TxSignature == "" || intent.SignedTx == "" {
		return RedeemPayoutRow{}, fmt.Errorf("tx signature and signed tx are required")
	}
	if intent.Amount <= 0 {
		return RedeemPayoutRow{}, fmt.Errorf("amount must be positive")
	}
	lastValidBlockHeight, err := blockHeightToBigint(intent.LastValidBlockHeight)
	if err != nil {
		return RedeemPayoutRow{}, err
	}

	tx, err := s.BeginTx(ctx)
	if err != nil {
		return RedeemPayoutRow{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	const updateJobSQL = `
UPDATE redeem_jobs
SET status = 'paying', updated_at = now()
WHERE id = $1 AND status IN ('debited', 'selling') AND withdrawal_id IS NULL`

	result, err := tx.ExecContext(ctx, updateJobSQL, intent.RedeemJobID)
	if err != nil {
		return RedeemPayoutRow{}, fmt.Errorf("mark redeem job paying: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return RedeemPayoutRow{}, fmt.Errorf("mark redeem job paying rows affected: %w", err)
	}
	if rows == 0 {
		return RedeemPayoutRow{}, fmt.Errorf("redeem job is not awaiting payout")
	}

	const insertSQL = `
INSERT INTO redeem_payouts (redeem_job_id, user_id, group_id, amount, to_address, tx_signature, signed_tx,
  last_valid_block_height, proof_message, proof_signature, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''), 'pending')
RETURNING ` + redeemPayoutColumns

	row, err := scanRedeemPayout(tx.QueryRowContext(ctx, insertSQL,
		intent.RedeemJobID, intent.UserID, intent.GroupID, intent.Amount, intent.ToAddress,
		intent.TxSignature, intent.SignedTx, lastValidBlockHeight,
		intent.ProofMessage, intent.ProofSignature,
	).Scan)
	if err != nil {
		if IsUniqueViolation(err) {
			return RedeemPayoutRow{}, ErrRedeemPayoutExists
		}
		return RedeemPayoutRow{}, fmt.Errorf("insert redeem payout: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RedeemPayoutRow{}, fmt.Errorf("commit redeem payout intent: %w", err)
	}
	committed = true
	return row, nil
}

// GetRedeemPayoutByJobID returns the payout attempt recorded for a redeem job, if any.
func (s *Store) GetRedeemPayoutByJobID(ctx context.Context, jobID string) (RedeemPayoutRow, bool, error) {
	if jobID == "" {
		return RedeemPayoutRow{}, false, fmt.Errorf("job id is required")
	}
	const selectSQL = `SELECT ` + redeemPayoutColumns + ` FROM redeem_payouts WHERE redeem_job_id = $1`

	row, err := scanRedeemPayout(s.db.QueryRowContext(ctx, selectSQL, jobID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return RedeemPayoutRow{}, false, nil
	}
	if err != nil {
		return RedeemPayoutRow{}, false, fmt.Errorf("get redeem payout: %w", err)
	}
	return row, true, nil
}

// LockRedeemPayoutByJobIDTx row-locks the payout attempt recorded for a redeem job, if any.
func (s *Store) LockRedeemPayoutByJobIDTx(ctx context.Context, tx *sql.Tx, jobID string) (RedeemPayoutRow, bool, error) {
	if jobID == "" {
		return RedeemPayoutRow{}, false, fmt.Errorf("job id is required")
	}
	const selectSQL = `SELECT ` + redeemPayoutColumns + ` FROM redeem_payouts WHERE redeem_job_id = $1 FOR UPDATE`

	row, err := scanRedeemPayout(tx.QueryRowContext(ctx, selectSQL, jobID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return RedeemPayoutRow{}, false, nil
	}
	if err != nil {
		return RedeemPayoutRow{}, false, fmt.Errorf("lock redeem payout: %w", err)
	}
	return row, true, nil
}

// ResolveRedeemPayoutTx moves a pending payout attempt to its final status. Only a pending
// attempt can be resolved, so a confirmed payout can never be re-labelled dropped or failed.
func (s *Store) ResolveRedeemPayoutTx(ctx context.Context, tx *sql.Tx, payoutID, status, failureReason string) error {
	if payoutID == "" {
		return fmt.Errorf("payout id is required")
	}
	switch status {
	case RedeemPayoutStatusConfirmed, RedeemPayoutStatusDropped, RedeemPayoutStatusFailed:
	default:
		return fmt.Errorf("invalid final redeem payout status %q", status)
	}

	const updateSQL = `
UPDATE redeem_payouts
SET status = $2, failure_reason = NULLIF($3, ''), updated_at = now()
WHERE id = $1 AND status = 'pending'`

	result, err := tx.ExecContext(ctx, updateSQL, payoutID, status, failureReason)
	if err != nil {
		return fmt.Errorf("resolve redeem payout: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve redeem payout rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("redeem payout is not pending")
	}
	return nil
}

// ReduceRedeemJobTx shrinks an unpaid job to the share units and USDC it will really settle
// for. The caller credits the difference back to the member in the same transaction.
func (s *Store) ReduceRedeemJobTx(ctx context.Context, tx *sql.Tx, jobID string, shareUnits, sliceUsdc int64) error {
	if jobID == "" {
		return fmt.Errorf("job id is required")
	}
	if shareUnits <= 0 || sliceUsdc <= 0 {
		return fmt.Errorf("share units and slice usdc must be positive")
	}

	const updateSQL = `
UPDATE redeem_jobs
SET share_units = $2, slice_usdc = $3, updated_at = now()
WHERE id = $1 AND status IN ('debited', 'selling') AND withdrawal_id IS NULL AND share_units >= $2`

	result, err := tx.ExecContext(ctx, updateSQL, jobID, shareUnits, sliceUsdc)
	if err != nil {
		return fmt.Errorf("reduce redeem job: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reduce redeem job rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("redeem job cannot be reduced")
	}
	return nil
}
