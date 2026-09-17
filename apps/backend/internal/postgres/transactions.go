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
// CostBasisPrice stores fill input (USDC spent); CostBasisAmount stores fill output (xStock received).
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
// ProceedsUSDC is stored in cost_basis_amount for the confirmed sell row.
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

// GetTransactionByID returns a transaction row by primary key.
func (s *Store) GetTransactionByID(ctx context.Context, id string) (TransactionRow, bool, error) {
	if id == "" {
		return TransactionRow{}, false, fmt.Errorf("transaction id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE id = $1`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, selectSQL, id).Scan(
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
		return TransactionRow{}, false, fmt.Errorf("get transaction by id: %w", err)
	}
	return row, true, nil
}

// TokenHoldingRow is a net treasury token balance derived from confirmed transactions.
type TokenHoldingRow struct {
	Mint   string
	Amount int64
}

// ListNetTokenHoldingsByGroup aggregates confirmed buy output minus sell input per mint.
func (s *Store) ListNetTokenHoldingsByGroup(ctx context.Context, groupID string) ([]TokenHoldingRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

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

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
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

// GetFillDerivedCostBasisByOutputMint returns cost basis from the latest confirmed buy fill.
func (s *Store) GetFillDerivedCostBasisByOutputMint(ctx context.Context, groupID, outputMint string) (int64, int64, bool, error) {
	if groupID == "" || outputMint == "" {
		return 0, 0, false, fmt.Errorf("group_id and output_mint are required")
	}

	const selectSQL = `
SELECT cost_basis_price, cost_basis_amount
FROM transactions
WHERE group_id = $1 AND output_mint = $2 AND action = 'buy' AND status = 'confirmed'
ORDER BY confirmed_at DESC NULLS LAST, created_at DESC
LIMIT 1`

	var price, amount sql.NullInt64
	err := s.db.QueryRowContext(ctx, selectSQL, groupID, outputMint).Scan(&price, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("get fill-derived cost basis: %w", err)
	}
	if !price.Valid || !amount.Valid {
		return 0, 0, false, nil
	}
	return price.Int64, amount.Int64, true, nil
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

// GetConfirmedTransactionByProposal returns the confirmed buy linked to a passed proposal.
func (s *Store) GetConfirmedTransactionByProposal(ctx context.Context, proposalID string) (TransactionRow, bool, error) {
	if proposalID == "" {
		return TransactionRow{}, false, fmt.Errorf("proposal_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE proposal_id = $1 AND action = 'buy' AND status = 'confirmed'
ORDER BY confirmed_at DESC NULLS LAST, created_at DESC
LIMIT 1`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, selectSQL, proposalID).Scan(
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
		return TransactionRow{}, false, fmt.Errorf("get transaction by proposal: %w", err)
	}
	return row, true, nil
}

// SetTransactionProposalID links a confirmed buy transaction to its passed proposal idempotently.
func (s *Store) SetTransactionProposalID(ctx context.Context, transactionID, proposalID string) (TransactionRow, bool, error) {
	if transactionID == "" || proposalID == "" {
		return TransactionRow{}, false, fmt.Errorf("transaction id and proposal id are required")
	}

	const updateSQL = `
UPDATE transactions
SET proposal_id = $2
WHERE id = $1 AND status = 'confirmed' AND (proposal_id IS NULL OR proposal_id = $2)
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, updateSQL, transactionID, proposalID).Scan(
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
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TransactionRow{}, false, fmt.Errorf("set transaction proposal id: %w", err)
	}

	existing, found, err := s.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return TransactionRow{}, false, err
	}
	if !found {
		return TransactionRow{}, false, fmt.Errorf("set transaction proposal id: transaction not found")
	}
	if existing.ProposalID.Valid && existing.ProposalID.String != proposalID {
		return TransactionRow{}, false, fmt.Errorf("set transaction proposal id: proposal mismatch")
	}
	return existing, false, nil
}
