package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// activeSwapPerProposalIndex is the partial unique index that allows one pending or
// confirmed swap per proposal and side (migration 000020).
const activeSwapPerProposalIndex = "transactions_one_active_swap_per_proposal"

// executeRequestIDConstraint is the unique constraint on transactions.execute_request_id.
const executeRequestIDConstraint = "transactions_execute_request_id_key"

// ErrActiveSwapExists means the proposal already has a pending or confirmed swap on this side.
var ErrActiveSwapExists = errors.New("active swap already exists for proposal")

// ErrSwapRequestRecorded means a row already exists for this venue request id, whatever its status.
var ErrSwapRequestRecorded = errors.New("swap request id already recorded")

const transactionColumns = `id, group_id, proposal_id, amount, action, input_mint, output_mint, status,
       tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, created_at, confirmed_at`

func scanTransactionRow(scanner interface{ Scan(dest ...any) error }, extra ...any) (TransactionRow, error) {
	var row TransactionRow
	dest := []any{
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
	}
	err := scanner.Scan(append(dest, extra...)...)
	return row, err
}

// InsertSwapIntentParams records a swap BEFORE it is submitted to the venue.
type InsertSwapIntentParams struct {
	GroupID          string
	ProposalID       string
	AgentIntentID    string
	InitiatedBy      string
	Action           string
	InputMint        string
	OutputMint       string
	Amount           int64
	Provider         string
	ExecuteRequestID string
	// SignedTxSignature and SignedTxBlockhash identify the signed Solana transaction when the
	// venue broadcasts the transaction we signed.
	SignedTxSignature string
	SignedTxBlockhash string
	// SubmitExpiresAt is when the venue stops accepting the signed payload. Zero when unknown.
	SubmitExpiresAt time.Time
}

// InsertSwapIntent persists a pending swap ahead of submission. It returns ErrActiveSwapExists
// when the proposal already has a pending or confirmed swap on this side, which is how
// concurrent executors are serialized, and ErrSwapRequestRecorded when the venue request id is
// not new. Either way the caller must not submit.
func (s *Store) InsertSwapIntent(ctx context.Context, params InsertSwapIntentParams) (TransactionRow, error) {
	if params.GroupID == "" || params.Action == "" || params.InputMint == "" || params.OutputMint == "" {
		return TransactionRow{}, fmt.Errorf("group_id, action, and mints are required")
	}
	if params.Amount <= 0 {
		return TransactionRow{}, fmt.Errorf("amount must be positive")
	}
	if params.Provider == "" || params.ExecuteRequestID == "" {
		return TransactionRow{}, fmt.Errorf("provider and execute_request_id are required")
	}

	initiatedBy := params.InitiatedBy
	if initiatedBy == "" {
		initiatedBy = "member_proposal"
	}

	const insertSQL = `
INSERT INTO transactions (
  group_id, proposal_id, agent_intent_id, initiated_by, amount, action, input_mint, output_mint,
  status, provider, execute_request_id, signed_tx_signature, signed_tx_blockhash, submit_expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, $10, $11, $12, $13)
RETURNING ` + transactionColumns

	var expiresAt sql.NullTime
	if !params.SubmitExpiresAt.IsZero() {
		expiresAt = sql.NullTime{Time: params.SubmitExpiresAt.UTC(), Valid: true}
	}

	row, err := scanTransactionRow(s.db.QueryRowContext(
		ctx,
		insertSQL,
		params.GroupID,
		nullString(params.ProposalID),
		nullString(params.AgentIntentID),
		initiatedBy,
		params.Amount,
		params.Action,
		params.InputMint,
		params.OutputMint,
		params.Provider,
		params.ExecuteRequestID,
		nullString(params.SignedTxSignature),
		nullString(params.SignedTxBlockhash),
		expiresAt,
	))
	if err != nil {
		if isUniqueViolationOn(err, activeSwapPerProposalIndex) {
			return TransactionRow{}, ErrActiveSwapExists
		}
		if isUniqueViolationOn(err, executeRequestIDConstraint) {
			return TransactionRow{}, fmt.Errorf("%w: %s", ErrSwapRequestRecorded, params.ExecuteRequestID)
		}
		return TransactionRow{}, fmt.Errorf("insert swap intent: %w", err)
	}
	return row, nil
}

// MarkSwapSubmitted records that the venue accepted the swap. executeRequestID replaces the
// pre-submit id when the venue only assigns its own id on accept.
func (s *Store) MarkSwapSubmitted(ctx context.Context, transactionID, executeRequestID string) error {
	if transactionID == "" || executeRequestID == "" {
		return fmt.Errorf("transaction id and execute_request_id are required")
	}
	const updateSQL = `
UPDATE transactions
SET submitted_at = now(), execute_request_id = $2
WHERE id = $1 AND status = 'pending'`
	if _, err := s.db.ExecContext(ctx, updateSQL, transactionID, executeRequestID); err != nil {
		return fmt.Errorf("mark swap submitted: %w", err)
	}
	return nil
}

// FailPendingSwap marks a pending swap definitively failed. It reports false when the row is
// no longer pending, so a swap that was confirmed meanwhile is never overwritten.
func (s *Store) FailPendingSwap(ctx context.Context, transactionID, reason string) (TransactionRow, bool, error) {
	if transactionID == "" {
		return TransactionRow{}, false, fmt.Errorf("transaction id is required")
	}
	const updateSQL = `
UPDATE transactions
SET status = 'failed', failure_reason = $2
WHERE id = $1 AND status = 'pending'
RETURNING ` + transactionColumns

	row, err := scanTransactionRow(s.db.QueryRowContext(ctx, updateSQL, transactionID, nullString(reason)))
	if errors.Is(err, sql.ErrNoRows) {
		return TransactionRow{}, false, nil
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("fail pending swap: %w", err)
	}
	return row, true, nil
}

// GetActiveSwapByProposalAndAction returns the pending or confirmed swap holding the
// proposal's execution slot, confirmed first.
func (s *Store) GetActiveSwapByProposalAndAction(ctx context.Context, proposalID, action string) (TransactionRow, bool, error) {
	if proposalID == "" || action == "" {
		return TransactionRow{}, false, fmt.Errorf("proposal_id and action are required")
	}
	const selectSQL = `
SELECT ` + transactionColumns + `
FROM transactions
WHERE proposal_id = $1 AND action = $2 AND status IN ('pending', 'confirmed')
ORDER BY (status = 'confirmed') DESC, created_at ASC
LIMIT 1`

	row, err := scanTransactionRow(s.db.QueryRowContext(ctx, selectSQL, proposalID, action))
	if errors.Is(err, sql.ErrNoRows) {
		return TransactionRow{}, false, nil
	}
	if err != nil {
		return TransactionRow{}, false, fmt.Errorf("get active swap by proposal: %w", err)
	}
	return row, true, nil
}

// SwapFill is the observed result of a swap, in atomic units of the row's input and output mints.
// InputAmount is required for buys only.
type SwapFill struct {
	TxSignature  string
	InputAmount  int64
	OutputAmount int64
}

// ConfirmPendingSwap records fill on the pending swap transactionID. It is idempotent: a row
// that is already confirmed is returned with created=false. A row that was marked failed is
// never flipped to confirmed here; that contradiction is returned as an error.
func (s *Store) ConfirmPendingSwap(ctx context.Context, transactionID string, fill SwapFill) (TransactionRow, bool, error) {
	if transactionID == "" || fill.TxSignature == "" {
		return TransactionRow{}, false, fmt.Errorf("transaction id and tx_signature are required")
	}
	if fill.OutputAmount <= 0 {
		return TransactionRow{}, false, fmt.Errorf("fill output amount must be positive")
	}

	row, found, err := s.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return TransactionRow{}, false, err
	}
	if !found {
		return TransactionRow{}, false, fmt.Errorf("swap transaction %s not found", transactionID)
	}
	switch row.Status {
	case TransactionStatusConfirmed:
		return row, false, nil
	case TransactionStatusPending:
	default:
		return TransactionRow{}, false, fmt.Errorf("swap transaction %s is %s but filled on chain as %s", transactionID, row.Status, fill.TxSignature)
	}

	switch row.Action {
	case TransactionActionBuy:
		// Cost basis is the USDC actually spent; sells only record their USDC proceeds.
		if fill.InputAmount <= 0 {
			return TransactionRow{}, false, fmt.Errorf("buy fill input amount must be positive")
		}
		return s.confirmPendingBuyTransaction(ctx, row.ID, ConfirmBuyTransactionParams{
			TxSignature:     fill.TxSignature,
			CostBasisPrice:  fill.InputAmount,
			CostBasisAmount: fill.OutputAmount,
		})
	case TransactionActionSell:
		return s.confirmPendingSellTransaction(ctx, row.ID, ConfirmSellTransactionParams{
			TxSignature:  fill.TxSignature,
			ProceedsUSDC: fill.OutputAmount,
		})
	default:
		return TransactionRow{}, false, fmt.Errorf("swap transaction %s has unsupported action %q", transactionID, row.Action)
	}
}

// PendingSwapRow is an unresolved swap plus what a reconciler needs to resolve it.
type PendingSwapRow struct {
	Transaction       TransactionRow
	Provider          sql.NullString
	SignedTxSignature sql.NullString
	SignedTxBlockhash sql.NullString
	SubmitExpiresAt   sql.NullTime
	SubmittedAt       sql.NullTime
}

const pendingSwapColumns = transactionColumns + `,
       provider, signed_tx_signature, signed_tx_blockhash, submit_expires_at, submitted_at`

func scanPendingSwapRow(scanner interface{ Scan(dest ...any) error }) (PendingSwapRow, error) {
	var pending PendingSwapRow
	tx, err := scanTransactionRow(scanner,
		&pending.Provider,
		&pending.SignedTxSignature,
		&pending.SignedTxBlockhash,
		&pending.SubmitExpiresAt,
		&pending.SubmittedAt,
	)
	pending.Transaction = tx
	return pending, err
}

// ListPendingSwaps returns pending swaps created at or before createdBefore, oldest first.
// Rows without a provider predate swap intents and carry nothing a reconciler could check.
func (s *Store) ListPendingSwaps(ctx context.Context, createdBefore time.Time, limit int) ([]PendingSwapRow, error) {
	if limit <= 0 {
		limit = 50
	}
	const selectSQL = `
SELECT ` + pendingSwapColumns + `
FROM transactions
WHERE status = 'pending' AND provider IS NOT NULL AND created_at <= $1
ORDER BY created_at ASC
LIMIT $2`

	rows, err := s.db.QueryContext(ctx, selectSQL, createdBefore.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending swaps: %w", err)
	}
	defer rows.Close()

	var out []PendingSwapRow
	for rows.Next() {
		pending, err := scanPendingSwapRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending swap: %w", err)
		}
		out = append(out, pending)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending swaps: %w", err)
	}
	return out, nil
}

// GetPendingSwapByID returns the pending swap transactionID, or false when the row does not
// exist or is no longer pending.
func (s *Store) GetPendingSwapByID(ctx context.Context, transactionID string) (PendingSwapRow, bool, error) {
	if transactionID == "" {
		return PendingSwapRow{}, false, fmt.Errorf("transaction id is required")
	}
	const selectSQL = `
SELECT ` + pendingSwapColumns + `
FROM transactions
WHERE id = $1 AND status = 'pending'`

	pending, err := scanPendingSwapRow(s.db.QueryRowContext(ctx, selectSQL, transactionID))
	if errors.Is(err, sql.ErrNoRows) {
		return PendingSwapRow{}, false, nil
	}
	if err != nil {
		return PendingSwapRow{}, false, fmt.Errorf("get pending swap: %w", err)
	}
	return pending, true, nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func isUniqueViolationOn(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
