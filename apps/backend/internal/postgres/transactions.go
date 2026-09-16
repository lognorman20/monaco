package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	TransactionActionBuy  = "buy"
	TransactionActionSell = "sell"

	TransactionStatusPending   = "pending"
	TransactionStatusConfirmed = "confirmed"
	TransactionStatusFailed    = "failed"
)

// TransactionRow is a row in transactions.
type TransactionRow struct {
	ID               string
	GroupID          string
	ProposalID       sql.NullString
	Amount           int64
	Action           string
	InputMint        string
	OutputMint       string
	Status           string
	TxSignature      sql.NullString
	ExecuteRequestID sql.NullString
	CostBasisPrice   sql.NullInt64
	CostBasisAmount  sql.NullInt64
	CreatedAt        time.Time
	ConfirmedAt      sql.NullTime
}

// ConfirmBuyTransactionParams persists a confirmed buy transaction idempotently.
type ConfirmBuyTransactionParams struct {
	GroupID          string
	Amount           int64
	InputMint        string
	OutputMint       string
	TxSignature      string
	ExecuteRequestID string
	CostBasisPrice   int64
	CostBasisAmount  int64
}

// ConfirmSellTransactionParams persists a confirmed sell transaction idempotently.
type ConfirmSellTransactionParams struct {
	GroupID          string
	Amount           int64
	InputMint        string
	OutputMint       string
	TxSignature      string
	ExecuteRequestID string
	ProceedsUSDC     int64
}

// InsertFailedTransaction records a terminal failed swap attempt.
func (s *Store) InsertFailedTransaction(ctx context.Context, groupID, action, inputMint, outputMint string, amount int64, executeRequestID string) (TransactionRow, error) {
	if groupID == "" || action == "" || inputMint == "" || outputMint == "" {
		return TransactionRow{}, fmt.Errorf("group_id, action, and mints are required")
	}
	if amount <= 0 {
		return TransactionRow{}, fmt.Errorf("amount must be positive")
	}

	const insertSQL = `
INSERT INTO transactions (group_id, amount, action, input_mint, output_mint, status, execute_request_id)
VALUES ($1, $2, $3, $4, $5, 'failed', $6)
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var row TransactionRow
	var executeID sql.NullString
	if executeRequestID != "" {
		executeID = sql.NullString{String: executeRequestID, Valid: true}
	}
	err := s.db.QueryRowContext(ctx, insertSQL, groupID, amount, action, inputMint, outputMint, executeID).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposalID,
		&row.Amount,
		&row.Action,
		&row.InputMint,
		&row.OutputMint,
		&row.Status,
		&row.TxSignature,
		&row.ExecuteRequestID,
		&row.CostBasisPrice,
		&row.CostBasisAmount,
		&row.CreatedAt,
		&row.ConfirmedAt,
	)
	if err != nil {
		return TransactionRow{}, fmt.Errorf("insert failed transaction: %w", err)
	}
	return row, nil
}

// ConfirmBuyTransaction upserts a confirmed buy by tx_signature or execute_request_id.
func (s *Store) ConfirmBuyTransaction(ctx context.Context, params ConfirmBuyTransactionParams) (TransactionRow, bool, error) {
	if params.GroupID == "" || params.TxSignature == "" {
		return TransactionRow{}, false, fmt.Errorf("group_id and tx_signature are required")
	}
	if params.Amount <= 0 || params.CostBasisAmount <= 0 {
		return TransactionRow{}, false, fmt.Errorf("amount and cost_basis_amount must be positive")
	}

	existing, found, err := s.GetConfirmedTransactionBySignature(ctx, params.TxSignature)
	if err != nil {
		return TransactionRow{}, false, err
	}
	if found {
		return existing, false, nil
	}
	if params.ExecuteRequestID != "" {
		existing, found, err = s.GetConfirmedTransactionByExecuteRequestID(ctx, params.ExecuteRequestID)
		if err != nil {
			return TransactionRow{}, false, err
		}
		if found {
			return existing, false, nil
		}
	}

	const insertSQL = `
INSERT INTO transactions (
  group_id, amount, action, input_mint, output_mint, status, tx_signature,
  execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at
)
VALUES ($1, $2, 'buy', $3, $4, 'confirmed', $5, $6, $7, $8, now())
ON CONFLICT (tx_signature) DO NOTHING
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var executeID sql.NullString
	if params.ExecuteRequestID != "" {
		executeID = sql.NullString{String: params.ExecuteRequestID, Valid: true}
	}

	var row TransactionRow
	err = s.db.QueryRowContext(
		ctx,
		insertSQL,
		params.GroupID,
		params.Amount,
		params.InputMint,
		params.OutputMint,
		params.TxSignature,
		executeID,
		params.CostBasisPrice,
		params.CostBasisAmount,
	).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposalID,
		&row.Amount,
		&row.Action,
		&row.InputMint,
		&row.OutputMint,
		&row.Status,
		&row.TxSignature,
		&row.ExecuteRequestID,
		&row.CostBasisPrice,
		&row.CostBasisAmount,
		&row.CreatedAt,
		&row.ConfirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, err := s.GetConfirmedTransactionBySignature(ctx, params.TxSignature)
		if err != nil {
			return TransactionRow{}, false, err
		}
		if found {
			return existing, false, nil
		}
		return TransactionRow{}, false, fmt.Errorf("confirm buy transaction: no row inserted")
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("confirm buy transaction: %w", err)
	}
	return row, true, nil
}

// ConfirmSellTransaction upserts a confirmed sell by tx_signature.
func (s *Store) ConfirmSellTransaction(ctx context.Context, params ConfirmSellTransactionParams) (TransactionRow, bool, error) {
	if params.GroupID == "" || params.TxSignature == "" {
		return TransactionRow{}, false, fmt.Errorf("group_id and tx_signature are required")
	}
	if params.Amount <= 0 || params.ProceedsUSDC <= 0 {
		return TransactionRow{}, false, fmt.Errorf("amount and proceeds must be positive")
	}

	existing, found, err := s.GetConfirmedTransactionBySignature(ctx, params.TxSignature)
	if err != nil {
		return TransactionRow{}, false, err
	}
	if found {
		return existing, false, nil
	}

	const insertSQL = `
INSERT INTO transactions (
  group_id, amount, action, input_mint, output_mint, status, tx_signature,
  execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at
)
VALUES ($1, $2, 'sell', $3, $4, 'confirmed', $5, $6, $7, $8, now())
ON CONFLICT (tx_signature) DO NOTHING
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var executeID sql.NullString
	if params.ExecuteRequestID != "" {
		executeID = sql.NullString{String: params.ExecuteRequestID, Valid: true}
	}

	var row TransactionRow
	err = s.db.QueryRowContext(
		ctx,
		insertSQL,
		params.GroupID,
		params.Amount,
		params.InputMint,
		params.OutputMint,
		params.TxSignature,
		executeID,
		params.ProceedsUSDC,
		params.ProceedsUSDC,
	).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposalID,
		&row.Amount,
		&row.Action,
		&row.InputMint,
		&row.OutputMint,
		&row.Status,
		&row.TxSignature,
		&row.ExecuteRequestID,
		&row.CostBasisPrice,
		&row.CostBasisAmount,
		&row.CreatedAt,
		&row.ConfirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, found, err := s.GetConfirmedTransactionBySignature(ctx, params.TxSignature)
		if err != nil {
			return TransactionRow{}, false, err
		}
		if found {
			return existing, false, nil
		}
		return TransactionRow{}, false, fmt.Errorf("confirm sell transaction: no row inserted")
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("confirm sell transaction: %w", err)
	}
	return row, true, nil
}

// GetConfirmedTransactionBySignature returns a confirmed transaction by signature.
func (s *Store) GetConfirmedTransactionBySignature(ctx context.Context, txSignature string) (TransactionRow, bool, error) {
	if txSignature == "" {
		return TransactionRow{}, false, fmt.Errorf("tx_signature is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE tx_signature = $1 AND status = 'confirmed'`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, selectSQL, txSignature).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposalID,
		&row.Amount,
		&row.Action,
		&row.InputMint,
		&row.OutputMint,
		&row.Status,
		&row.TxSignature,
		&row.ExecuteRequestID,
		&row.CostBasisPrice,
		&row.CostBasisAmount,
		&row.CreatedAt,
		&row.ConfirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TransactionRow{}, false, nil
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("get transaction by signature: %w", err)
	}
	return row, true, nil
}

// GetConfirmedTransactionByExecuteRequestID returns a confirmed transaction by execute request id.
func (s *Store) GetConfirmedTransactionByExecuteRequestID(ctx context.Context, executeRequestID string) (TransactionRow, bool, error) {
	if executeRequestID == "" {
		return TransactionRow{}, false, fmt.Errorf("execute_request_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE execute_request_id = $1 AND status = 'confirmed'`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, selectSQL, executeRequestID).Scan(
		&row.ID,
		&row.GroupID,
		&row.ProposalID,
		&row.Amount,
		&row.Action,
		&row.InputMint,
		&row.OutputMint,
		&row.Status,
		&row.TxSignature,
		&row.ExecuteRequestID,
		&row.CostBasisPrice,
		&row.CostBasisAmount,
		&row.CreatedAt,
		&row.ConfirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TransactionRow{}, false, nil
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("get transaction by execute_request_id: %w", err)
	}
	return row, true, nil
}

// CountConfirmedTransactionsBySignature counts confirmed rows for a signature.
func (s *Store) CountConfirmedTransactionsBySignature(ctx context.Context, txSignature string) (int, error) {
	if txSignature == "" {
		return 0, fmt.Errorf("tx_signature is required")
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM transactions WHERE tx_signature = $1 AND status = 'confirmed'`, txSignature).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count transactions by signature: %w", err)
	}
	return count, nil
}
