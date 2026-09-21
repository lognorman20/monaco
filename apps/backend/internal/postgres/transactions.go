package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TransactionActionBuy  = "buy"
	TransactionActionSell = "sell"

	TransactionStatusPending   = "pending"
	TransactionStatusConfirmed = "confirmed"
	TransactionStatusFailed    = "failed"
)

// SwapUsdcMicros reports what a swap row is worth in USDC micros, and whether
// that figure is known at all.
//
// transactions.amount is the swap's INPUT amount, so its unit depends on the
// direction: a buy spends USDC and stores micros, but a sell spends the xStock
// and stores token atomics (jupiter.XStockAtomicScale per share). Printing a
// sell's amount as dollars is off by the ratio of the two scales, which is why
// every caller that shows money must go through here instead of reading Amount.
//
// A confirmed sell carries its USDC proceeds in cost_basis_amount, and that is
// the only honest dollar figure for a sell. A sell that has not confirmed — or
// an older row that never recorded proceeds — has no USDC figure, and the
// second return is false so callers can say "12 AAPLx" rather than invent a
// price.
func SwapUsdcMicros(action, status string, amount int64, costBasisAmount sql.NullInt64) (int64, bool) {
	if !strings.EqualFold(strings.TrimSpace(action), TransactionActionSell) {
		return amount, true
	}
	if strings.EqualFold(strings.TrimSpace(status), TransactionStatusConfirmed) && costBasisAmount.Valid {
		return costBasisAmount.Int64, true
	}
	return 0, false
}

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

// InsertPendingTransactionParams records an in-flight swap before Jupiter confirms.
type InsertPendingTransactionParams struct {
	GroupID          string
	ProposalID       string
	AgentIntentID    string
	InitiatedBy      string
	Action           string
	InputMint        string
	OutputMint       string
	Amount           int64
	ExecuteRequestID string
}

// InsertPendingTransaction persists a pending swap row idempotently on execute_request_id.
func (s *Store) InsertPendingTransaction(ctx context.Context, params InsertPendingTransactionParams) (TransactionRow, bool, error) {
	if params.GroupID == "" || params.Action == "" || params.InputMint == "" || params.OutputMint == "" {
		return TransactionRow{}, false, fmt.Errorf("group_id, action, and mints are required")
	}
	if params.Amount <= 0 {
		return TransactionRow{}, false, fmt.Errorf("amount must be positive")
	}
	if params.ExecuteRequestID == "" {
		return TransactionRow{}, false, fmt.Errorf("execute_request_id is required")
	}

	if existing, found, err := s.GetTransactionByExecuteRequestID(ctx, params.ExecuteRequestID); err != nil {
		return TransactionRow{}, false, err
	} else if found {
		return existing, false, nil
	}

	initiatedBy := params.InitiatedBy
	if initiatedBy == "" {
		initiatedBy = "member_proposal"
	}

	const insertSQL = `
INSERT INTO transactions (group_id, proposal_id, agent_intent_id, initiated_by, amount, action, input_mint, output_mint, status, execute_request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9)
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var proposalID sql.NullString
	if params.ProposalID != "" {
		proposalID = sql.NullString{String: params.ProposalID, Valid: true}
	}
	var agentIntentID sql.NullString
	if params.AgentIntentID != "" {
		agentIntentID = sql.NullString{String: params.AgentIntentID, Valid: true}
	}

	var row TransactionRow
	err := s.db.QueryRowContext(
		ctx,
		insertSQL,
		params.GroupID,
		proposalID,
		agentIntentID,
		initiatedBy,
		params.Amount,
		params.Action,
		params.InputMint,
		params.OutputMint,
		params.ExecuteRequestID,
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
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("insert pending transaction: %w", err)
	}
	return row, true, nil
}

// FailTransactionByExecuteRequestID marks a pending swap failed.
func (s *Store) FailTransactionByExecuteRequestID(ctx context.Context, executeRequestID string) (TransactionRow, bool, error) {
	if executeRequestID == "" {
		return TransactionRow{}, false, fmt.Errorf("execute_request_id is required")
	}

	const updateSQL = `
UPDATE transactions
SET status = 'failed'
WHERE execute_request_id = $1 AND status = 'pending'
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var row TransactionRow
	err := s.db.QueryRowContext(ctx, updateSQL, executeRequestID).Scan(
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
		return TransactionRow{}, false, fmt.Errorf("fail transaction by execute_request_id: %w", err)
	}
	return row, true, nil
}

// GetTransactionByExecuteRequestID returns a transaction row for an execute request id.
func (s *Store) GetTransactionByExecuteRequestID(ctx context.Context, executeRequestID string) (TransactionRow, bool, error) {
	if executeRequestID == "" {
		return TransactionRow{}, false, fmt.Errorf("execute_request_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE execute_request_id = $1`

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
		pending, pendingFound, err := s.GetTransactionByExecuteRequestID(ctx, params.ExecuteRequestID)
		if err != nil {
			return TransactionRow{}, false, err
		}
		if pendingFound && pending.Status == TransactionStatusPending {
			return s.confirmPendingBuyTransaction(ctx, pending.ID, params)
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
	if params.ExecuteRequestID != "" {
		pending, pendingFound, err := s.GetTransactionByExecuteRequestID(ctx, params.ExecuteRequestID)
		if err != nil {
			return TransactionRow{}, false, err
		}
		if pendingFound && pending.Status == TransactionStatusPending {
			return s.confirmPendingSellTransaction(ctx, pending.ID, params)
		}
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

// TransactionActivityRow is a transaction row enriched for group activity feeds.
type TransactionActivityRow struct {
	TransactionRow
	InitiatedBy      string
	AgentDisplayName string
}

// ListTransactionActivityByGroupID returns transactions for activity with agent attribution.
func (s *Store) ListTransactionActivityByGroupID(ctx context.Context, groupID string) ([]TransactionActivityRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT t.id, t.group_id, t.proposal_id, t.amount, t.action, t.input_mint, t.output_mint, t.status,
       t.tx_signature, t.execute_request_id, t.cost_basis_price, t.cost_basis_amount, t.created_at, t.confirmed_at,
       COALESCE(t.initiated_by, ''), COALESCE(ga.agent_display_name, '')
FROM transactions t
LEFT JOIN agent_intents ai ON ai.id = t.agent_intent_id
LEFT JOIN group_agents ga ON ga.id = ai.group_agent_id
WHERE t.group_id = $1
ORDER BY t.created_at DESC
LIMIT 100`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list transaction activity by group: %w", err)
	}
	defer rows.Close()

	var out []TransactionActivityRow
	for rows.Next() {
		var row TransactionActivityRow
		if err := rows.Scan(
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
			&row.InitiatedBy,
			&row.AgentDisplayName,
		); err != nil {
			return nil, fmt.Errorf("scan transaction activity: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListTransactionsByGroupID returns transactions for a group newest first.
func (s *Store) ListTransactionsByGroupID(ctx context.Context, groupID string) ([]TransactionRow, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE group_id = $1
ORDER BY created_at DESC
LIMIT 100`

	rows, err := s.db.QueryContext(ctx, selectSQL, groupID)
	if err != nil {
		return nil, fmt.Errorf("list transactions by group: %w", err)
	}
	defer rows.Close()

	var out []TransactionRow
	for rows.Next() {
		var row TransactionRow
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
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
	return fillDerivedCostBasisByOutputMintQuery(ctx, s.db, groupID, outputMint)
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

// GetLatestBuyTransactionByProposal returns the newest buy row linked to proposalID, any status.
func (s *Store) GetLatestBuyTransactionByProposal(ctx context.Context, proposalID string) (TransactionRow, bool, error) {
	if proposalID == "" {
		return TransactionRow{}, false, fmt.Errorf("proposal_id is required")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE proposal_id = $1 AND action = 'buy'
ORDER BY created_at DESC
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
		return TransactionRow{}, false, fmt.Errorf("get latest buy transaction by proposal: %w", err)
	}
	return row, true, nil
}

// GetConfirmedTransactionByProposal returns the confirmed buy linked to a passed proposal.
func (s *Store) GetConfirmedTransactionByProposal(ctx context.Context, proposalID string) (TransactionRow, bool, error) {
	return s.GetConfirmedTransactionByProposalAndAction(ctx, proposalID, TransactionActionBuy)
}

func (s *Store) GetConfirmedTransactionByProposalAndAction(ctx context.Context, proposalID, action string) (TransactionRow, bool, error) {
	if proposalID == "" {
		return TransactionRow{}, false, fmt.Errorf("proposal_id is required")
	}
	if action != TransactionActionBuy && action != TransactionActionSell {
		return TransactionRow{}, false, fmt.Errorf("invalid transaction action")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE proposal_id = $1 AND action = $2 AND status = 'confirmed'
ORDER BY confirmed_at DESC NULLS LAST, created_at DESC
LIMIT 1`

	return scanTransactionByProposalQuery(ctx, s, selectSQL, proposalID, action, "get transaction by proposal and action")
}

func (s *Store) GetLatestTransactionByProposalAndAction(ctx context.Context, proposalID, action string) (TransactionRow, bool, error) {
	if proposalID == "" {
		return TransactionRow{}, false, fmt.Errorf("proposal_id is required")
	}
	if action != TransactionActionBuy && action != TransactionActionSell {
		return TransactionRow{}, false, fmt.Errorf("invalid transaction action")
	}

	const selectSQL = `
SELECT id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at
FROM transactions
WHERE proposal_id = $1 AND action = $2
ORDER BY created_at DESC
LIMIT 1`

	return scanTransactionByProposalQuery(ctx, s, selectSQL, proposalID, action, "get latest transaction by proposal and action")
}

func scanTransactionByProposalQuery(ctx context.Context, s *Store, selectSQL, proposalID, action, errLabel string) (TransactionRow, bool, error) {
	var row TransactionRow
	err := s.db.QueryRowContext(ctx, selectSQL, proposalID, action).Scan(
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
		return TransactionRow{}, false, fmt.Errorf("%s: %w", errLabel, err)
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

func (s *Store) confirmPendingBuyTransaction(ctx context.Context, transactionID string, params ConfirmBuyTransactionParams) (TransactionRow, bool, error) {
	const updateSQL = `
UPDATE transactions
SET status = 'confirmed',
    tx_signature = $2,
    cost_basis_price = $3,
    cost_basis_amount = $4,
    confirmed_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var row TransactionRow
	err := s.db.QueryRowContext(
		ctx,
		updateSQL,
		transactionID,
		params.TxSignature,
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
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("confirm pending buy transaction: %w", err)
	}
	return row, true, nil
}

func (s *Store) confirmPendingSellTransaction(ctx context.Context, transactionID string, params ConfirmSellTransactionParams) (TransactionRow, bool, error) {
	const updateSQL = `
UPDATE transactions
SET status = 'confirmed',
    tx_signature = $2,
    cost_basis_price = $3,
    cost_basis_amount = $4,
    confirmed_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
          tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

	var row TransactionRow
	err := s.db.QueryRowContext(
		ctx,
		updateSQL,
		transactionID,
		params.TxSignature,
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
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("confirm pending sell transaction: %w", err)
	}
	return row, true, nil
}
