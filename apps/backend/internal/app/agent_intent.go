package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

const (
	// MaxAgentIdempotencyKeyLength bounds the client-supplied idempotency key.
	MaxAgentIdempotencyKeyLength = 128

	// agentIntentAbandonedAfter is how long an accepted intent with no ledger row keeps its
	// reservation. It is far past the API's 3 minute write timeout, so only an intent whose
	// process died before the swap was recorded ever reaches it.
	agentIntentAbandonedAfter = 15 * time.Minute

	agentIntentStatusWriteAttempts = 3
	agentIntentStatusWriteTimeout  = 5 * time.Second
	agentIntentStatusWriteBackoff  = 200 * time.Millisecond
)

// AgentIntentService executes agent trade intents via the existing treasury swap path.
type AgentIntentService struct {
	store   *postgres.Store
	swap    *SwapService
	symbols *SymbolResolver
}

// NewAgentIntentService wires agent intent execution.
func NewAgentIntentService(store *postgres.Store, swap *SwapService, symbols *SymbolResolver) *AgentIntentService {
	return &AgentIntentService{store: store, swap: swap, symbols: symbols}
}

// SubmitAgentIntentInput is a normalized agent trade intent.
type SubmitAgentIntentInput struct {
	GroupID     string
	AgentKey    string
	Side        domain.AgentIntentSide
	Symbol      string
	UsdcMicros  int64
	TokenAmount int64
	// IdempotencyKey is optional and unique per agent. A resend under the same key returns
	// the first intent's outcome instead of trading again.
	IdempotencyKey string
}

// SubmitAgentIntentResult is the persisted intent and optional transaction.
type SubmitAgentIntentResult struct {
	IntentID      string
	Status        string
	TransactionID string
	RejectReason  string
}

// SubmitAgentIntent authenticates the agent key and runs a treasury swap when valid.
func (s *AgentIntentService) SubmitAgentIntent(ctx context.Context, in SubmitAgentIntentInput) (SubmitAgentIntentResult, error) {
	if in.GroupID == "" {
		return SubmitAgentIntentResult{}, fmt.Errorf("group id is required")
	}
	if in.AgentKey == "" {
		return SubmitAgentIntentResult{}, ErrInvalidAgentAPIKey
	}
	// Faker scale clubs (#153) are read-only: no agent trading, and never a Privy treasury
	// balance read for the snapshot below.
	if err := rejectFakerGroup(ctx, s.store, in.GroupID); err != nil {
		return SubmitAgentIntentResult{}, err
	}

	keyHash := HashAgentAPIKey(in.AgentKey)
	agentRow, found, err := s.store.GetGroupAgentByAPIKeyHash(ctx, keyHash)
	if err != nil {
		return SubmitAgentIntentResult{}, err
	}
	if !found {
		return SubmitAgentIntentResult{}, ErrInvalidAgentAPIKey
	}
	if agentRow.GroupID != in.GroupID {
		return SubmitAgentIntentResult{}, ErrAgentGroupMismatch
	}
	if agentRow.Status == domain.AgentStatusRevoked || !agentRow.APIKeyHash.Valid {
		return SubmitAgentIntentResult{}, ErrInvalidAgentAPIKey
	}
	if len(in.IdempotencyKey) > MaxAgentIdempotencyKeyLength {
		return SubmitAgentIntentResult{}, fmt.Errorf("%w: idempotency key is longer than %d characters", ErrAgentIntentRejected, MaxAgentIdempotencyKeyLength)
	}

	accepted, answer, err := s.reserveIntent(ctx, agentRow.ID, keyHash, in)
	if err != nil || accepted.ID == "" {
		return answer, err
	}

	execResult, execErr := s.executeIntent(ctx, accepted, in)
	if execErr != nil {
		status := "failed"
		if errors.Is(execErr, ErrAgentIntentRejected) {
			status = "rejected"
		}
		s.recordIntentOutcome(accepted.ID, status, execErr.Error(), execResult.TransactionID)
		return SubmitAgentIntentResult{
			IntentID:     accepted.ID,
			Status:       status,
			RejectReason: execErr.Error(),
		}, execErr
	}
	s.recordIntentOutcome(accepted.ID, "executed", "", execResult.TransactionID)
	return execResult, nil
}

// reserveIntent decides one intent under the agent's row lock. When it returns an accepted
// row, that row is committed and already counts against the agent's budget (buy) or position
// (sell), so a concurrent intent that takes the lock next sees it. Every other outcome comes
// back as an answer with no accepted row: the replay of an earlier intent under the same
// idempotency key, or a rejection.
func (s *AgentIntentService) reserveIntent(ctx context.Context, agentID, keyHash string, in SubmitAgentIntentInput) (postgres.AgentIntentRow, SubmitAgentIntentResult, error) {
	none := postgres.AgentIntentRow{}

	// Everything that needs the network happens before the lock is taken.
	var sellMint string
	var treasuryUSDC int64
	var unknownSymbol error
	switch in.Side {
	case domain.AgentIntentSell:
		mint, err := s.swap.buy.ResolveOutputMint(ctx, in.Symbol)
		switch {
		case errors.Is(err, xstocks.ErrNotFound):
			unknownSymbol = fmt.Errorf("unknown symbol")
		case err != nil:
			return none, SubmitAgentIntentResult{}, err
		}
		sellMint = mint
	case domain.AgentIntentBuy:
		usdc, err := s.treasuryUSDC(ctx, in.GroupID)
		if err != nil {
			return none, SubmitAgentIntentResult{}, err
		}
		treasuryUSDC = usdc
	}

	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return none, SubmitAgentIntentResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	agentRow, found, err := s.store.LockGroupAgentTx(ctx, tx, agentID)
	if err != nil {
		return none, SubmitAgentIntentResult{}, err
	}
	// A revoke that landed between the key lookup and the lock wins.
	if !found || agentRow.Status == domain.AgentStatusRevoked || !agentRow.APIKeyHash.Valid || agentRow.APIKeyHash.String != keyHash {
		return none, SubmitAgentIntentResult{}, ErrInvalidAgentAPIKey
	}
	if err := s.store.ReconcileAgentIntentsTx(ctx, tx, agentRow.ID, agentIntentAbandonedAfter); err != nil {
		return none, SubmitAgentIntentResult{}, err
	}

	if in.IdempotencyKey != "" {
		earlier, found, err := s.store.GetAgentIntentByIdempotencyKeyTx(ctx, tx, agentRow.ID, in.IdempotencyKey)
		if err != nil {
			return none, SubmitAgentIntentResult{}, err
		}
		if found {
			// Keep what the reconcile pass repaired; the replay below reads from it.
			if err := tx.Commit(); err != nil {
				return none, SubmitAgentIntentResult{}, err
			}
			answer, err := replayAgentIntent(earlier, in)
			return none, answer, err
		}
	}

	if agentRow.Status == domain.AgentStatusPaused {
		return none, SubmitAgentIntentResult{}, ErrAgentPaused
	}
	if agentRow.Status != domain.AgentStatusActive {
		return none, SubmitAgentIntentResult{}, ErrAgentIntentRejected
	}

	agent := groupAgentFromRow(agentRow)
	validationErr := unknownSymbol
	if validationErr == nil {
		snap, err := s.buildAgentSnapshotTx(ctx, tx, agent, in, sellMint, treasuryUSDC)
		if err != nil {
			return none, SubmitAgentIntentResult{}, err
		}
		validationErr = domain.ValidateIntent(agent, domain.AgentIntentRequest{
			Side:        in.Side,
			Symbol:      in.Symbol,
			UsdcMicros:  in.UsdcMicros,
			TokenAmount: in.TokenAmount,
		}, snap)
	}

	row := postgres.AgentIntentRow{
		GroupAgentID:   agent.ID,
		GroupID:        in.GroupID,
		Side:           in.Side,
		Symbol:         in.Symbol,
		UsdcMicros:     nullInt64(in.UsdcMicros),
		TokenAmount:    nullInt64(in.TokenAmount),
		Status:         "accepted",
		IdempotencyKey: nullString(in.IdempotencyKey),
		Mint:           nullString(sellMint),
	}
	if validationErr != nil {
		row.Status = "rejected"
		row.RejectReason = sql.NullString{String: validationErr.Error(), Valid: true}
	}
	inserted, err := s.store.InsertAgentIntentTx(ctx, tx, row)
	if err != nil {
		return none, SubmitAgentIntentResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return none, SubmitAgentIntentResult{}, err
	}
	if validationErr != nil {
		return none, SubmitAgentIntentResult{
			IntentID:     inserted.ID,
			Status:       "rejected",
			RejectReason: validationErr.Error(),
		}, fmt.Errorf("%w: %v", ErrAgentIntentRejected, validationErr)
	}
	return inserted, SubmitAgentIntentResult{}, nil
}

// replayAgentIntent answers a resend under an idempotency key with the first intent's outcome.
func replayAgentIntent(earlier postgres.AgentIntentRow, in SubmitAgentIntentInput) (SubmitAgentIntentResult, error) {
	if earlier.Side != in.Side || earlier.Symbol != in.Symbol ||
		earlier.UsdcMicros != nullInt64(in.UsdcMicros) || earlier.TokenAmount != nullInt64(in.TokenAmount) {
		return SubmitAgentIntentResult{}, fmt.Errorf("%w: idempotency key was already used for a different intent", ErrAgentIntentRejected)
	}
	answer := SubmitAgentIntentResult{
		IntentID:      earlier.ID,
		Status:        earlier.Status,
		TransactionID: earlier.TransactionID.String,
	}
	switch earlier.Status {
	case "rejected":
		answer.RejectReason = earlier.RejectReason.String
		return answer, fmt.Errorf("%w: %s", ErrAgentIntentRejected, earlier.RejectReason.String)
	case "accepted":
		return answer, ErrAgentIntentInFlight
	case "failed":
		// The stored reason is an internal error; the first answer did not expose it either.
		answer.RejectReason = "execution failed"
	}
	return answer, nil
}

// recordIntentOutcome writes the final status of an intent. It runs on its own context
// because the request's may already be cancelled (bot timeout) by the time the swap returns.
// A write that still fails is logged with what is needed to repair the row, and
// ReconcileAgentIntentsTx repairs it from the ledger on the agent's next intent.
func (s *AgentIntentService) recordIntentOutcome(intentID, status, reason, transactionID string) {
	var err error
	for attempt := 1; attempt <= agentIntentStatusWriteAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), agentIntentStatusWriteTimeout)
		err = s.store.UpdateAgentIntentStatus(ctx, intentID, status, reason, transactionID)
		cancel()
		if err == nil {
			return
		}
		if attempt < agentIntentStatusWriteAttempts {
			time.Sleep(time.Duration(attempt) * agentIntentStatusWriteBackoff)
		}
	}
	logAgentIntentStatusWriteFailed(intentID, status, transactionID, err)
}

func (s *AgentIntentService) executeIntent(ctx context.Context, accepted postgres.AgentIntentRow, in SubmitAgentIntentInput) (SubmitAgentIntentResult, error) {
	switch in.Side {
	case domain.AgentIntentBuy:
		result, err := s.swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{
			GroupID:       in.GroupID,
			Symbol:        in.Symbol,
			USDCAmount:    in.UsdcMicros,
			AgentIntentID: accepted.ID,
			InitiatedBy:   "agent",
		})
		if err != nil {
			return SubmitAgentIntentResult{IntentID: accepted.ID}, err
		}
		return SubmitAgentIntentResult{
			IntentID:      accepted.ID,
			Status:        "executed",
			TransactionID: result.Transaction.ID,
		}, nil
	case domain.AgentIntentSell:
		inputMint, err := s.swap.buy.ResolveOutputMint(ctx, in.Symbol)
		if err != nil {
			return SubmitAgentIntentResult{IntentID: accepted.ID}, err
		}
		result, err := s.swap.SellToUSDC(ctx, SellToUSDCRequest{
			GroupID:       in.GroupID,
			Symbol:        in.Symbol,
			InputMint:     inputMint,
			Amount:        in.TokenAmount,
			AgentIntentID: accepted.ID,
			InitiatedBy:   "agent",
		})
		if err != nil {
			return SubmitAgentIntentResult{IntentID: accepted.ID}, err
		}
		return SubmitAgentIntentResult{
			IntentID:      accepted.ID,
			Status:        "executed",
			TransactionID: result.Transaction.ID,
		}, nil
	default:
		return SubmitAgentIntentResult{IntentID: accepted.ID}, fmt.Errorf("invalid intent side")
	}
}

// treasuryUSDC reads the on-chain USDC a buy can draw on.
func (s *AgentIntentService) treasuryUSDC(ctx context.Context, groupID string) (int64, error) {
	treasury, found, err := s.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if !found || s.swap == nil || s.swap.privy == nil {
		return 0, nil
	}
	return s.swap.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
}

// buildAgentSnapshotTx reads the agent's budget or position inside the tx that holds its lock.
func (s *AgentIntentService) buildAgentSnapshotTx(ctx context.Context, tx *sql.Tx, agent domain.GroupAgent, in SubmitAgentIntentInput, sellMint string, treasuryUSDC int64) (domain.AgentTreasurySnapshot, error) {
	snap := domain.AgentTreasurySnapshot{TreasuryUsdcMicros: treasuryUSDC}
	switch in.Side {
	case domain.AgentIntentBuy:
		committed, err := s.store.SumAgentCommittedBuyUSDCTx(ctx, tx, agent.ID)
		if err != nil {
			return domain.AgentTreasurySnapshot{}, err
		}
		snap.AgentCommittedUsdcMicros = committed
	case domain.AgentIntentSell:
		held, err := s.store.NetTokenHoldingByGroupAndMintTx(ctx, tx, agent.GroupID, sellMint)
		if err != nil {
			return domain.AgentTreasurySnapshot{}, err
		}
		own, err := s.store.AgentSellableTokenAmountTx(ctx, tx, agent.ID, sellMint)
		if err != nil {
			return domain.AgentTreasurySnapshot{}, err
		}
		snap.TokenHoldingsBySymbol = map[string]int64{in.Symbol: held}
		snap.AgentTokenHoldingsBySymbol = map[string]int64{in.Symbol: own}
	}
	return snap, nil
}

func nullInt64(v int64) sql.NullInt64 {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
