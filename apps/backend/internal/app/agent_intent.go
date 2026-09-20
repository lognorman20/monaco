package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
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
	if agentRow.Status == domain.AgentStatusPaused {
		return SubmitAgentIntentResult{}, ErrAgentPaused
	}
	if agentRow.Status != domain.AgentStatusActive {
		return SubmitAgentIntentResult{}, ErrAgentIntentRejected
	}

	agent := groupAgentFromRow(agentRow)
	snap, err := s.buildAgentSnapshot(ctx, agent)
	if err != nil {
		return SubmitAgentIntentResult{}, err
	}
	intent := domain.AgentIntentRequest{
		Side:        in.Side,
		Symbol:      in.Symbol,
		UsdcMicros:  in.UsdcMicros,
		TokenAmount: in.TokenAmount,
	}
	if err := domain.ValidateIntent(agent, intent, snap); err != nil {
		rejected, insertErr := s.store.InsertAgentIntent(ctx, postgres.AgentIntentRow{
			GroupAgentID: agent.ID,
			GroupID:      in.GroupID,
			Side:         in.Side,
			Symbol:       in.Symbol,
			UsdcMicros:   nullInt64(in.UsdcMicros),
			TokenAmount:  nullInt64(in.TokenAmount),
			Status:       "rejected",
			RejectReason: sql.NullString{String: err.Error(), Valid: true},
		})
		if insertErr != nil {
			return SubmitAgentIntentResult{}, insertErr
		}
		return SubmitAgentIntentResult{
			IntentID:     rejected.ID,
			Status:       "rejected",
			RejectReason: err.Error(),
		}, fmt.Errorf("%w: %v", ErrAgentIntentRejected, err)
	}

	accepted, err := s.store.InsertAgentIntent(ctx, postgres.AgentIntentRow{
		GroupAgentID: agent.ID,
		GroupID:      in.GroupID,
		Side:         in.Side,
		Symbol:       in.Symbol,
		UsdcMicros:   nullInt64(in.UsdcMicros),
		TokenAmount:  nullInt64(in.TokenAmount),
		Status:       "accepted",
	})
	if err != nil {
		return SubmitAgentIntentResult{}, err
	}

	execResult, execErr := s.executeIntent(ctx, accepted, in)
	if execErr != nil {
		status := "failed"
		if errors.Is(execErr, ErrAgentIntentRejected) {
			status = "rejected"
		}
		_ = s.store.UpdateAgentIntentStatus(context.Background(), accepted.ID, status, execErr.Error(), execResult.TransactionID)
		return SubmitAgentIntentResult{
			IntentID:     accepted.ID,
			Status:       status,
			RejectReason: execErr.Error(),
		}, execErr
	}
	_ = s.store.UpdateAgentIntentStatus(context.Background(), accepted.ID, "executed", "", execResult.TransactionID)
	return execResult, nil
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
		inputMint, err := s.swap.buy.ResolveOutputToken(ctx, in.Symbol)
		if err != nil {
			return SubmitAgentIntentResult{IntentID: accepted.ID}, err
		}
		result, err := s.swap.SellToUSDC(ctx, SellToUSDCRequest{
			GroupID:       in.GroupID,
			Symbol:        in.Symbol,
			InputToken:    inputMint,
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

func (s *AgentIntentService) buildAgentSnapshot(ctx context.Context, agent domain.GroupAgent) (domain.AgentTreasurySnapshot, error) {
	var treasuryUSDC int64
	treasury, found, err := s.store.GetTreasuryByGroupID(ctx, agent.GroupID)
	if err != nil {
		return domain.AgentTreasurySnapshot{}, err
	}
	if found && s.swap != nil && s.swap.wallets != nil {
		treasuryUSDC, err = s.swap.wallets.TreasuryUSDCBalance(ctx, treasury.Address)
		if err != nil {
			return domain.AgentTreasurySnapshot{}, err
		}
	}
	spent, err := s.store.SumAgentExecutedBuyUSDCByAgentID(ctx, agent.ID)
	if err != nil {
		return domain.AgentTreasurySnapshot{}, err
	}
	pendingAgent, err := s.store.SumPendingAgentBuyUSDCByAgentID(ctx, agent.ID)
	if err != nil {
		return domain.AgentTreasurySnapshot{}, err
	}
	holdings, err := s.store.ListNetTokenHoldingsByGroup(ctx, agent.GroupID)
	if err != nil {
		return domain.AgentTreasurySnapshot{}, err
	}
	bySymbol := make(map[string]int64, len(holdings))
	for _, holding := range holdings {
		symbol := symbolForOutputToken(ctx, s.symbols, holding.Mint)
		bySymbol[symbol] += holding.Amount
	}
	return domain.AgentTreasurySnapshot{
		TreasuryUsdcMicros:     treasuryUSDC,
		AgentSpentUsdcMicros:   spent,
		PendingAgentUsdcMicros: pendingAgent,
		TokenHoldingsBySymbol:  bySymbol,
	}, nil
}

func nullInt64(v int64) sql.NullInt64 {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
