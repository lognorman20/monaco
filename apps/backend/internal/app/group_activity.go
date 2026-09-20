package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// GroupActivityItem is one row in GET /v1/groups/{id}/activity.
type GroupActivityItem struct {
	ID                 string
	Kind               string
	Status             string
	Symbol             string
	AmountMicros       int64
	TokenAmount        int64
	ProceedsUsdcMicros int64
	CreatedAt          time.Time
	TxHash        string
	InitiatedBy        string
	AgentDisplayName   string
}

// ListGroupActivity returns deposits and treasury swaps for a group member.
func (h *HomeService) ListGroupActivity(ctx context.Context, accessToken, groupID string) ([]GroupActivityItem, error) {
	if strings.TrimSpace(groupID) == "" {
		return nil, fmt.Errorf("group id is required")
	}

	// Members read their club; any authed user may spectate a faker scale club (#153).
	if _, err := authorizeGroupReader(ctx, h.store, h.privy, accessToken, groupID); err != nil {
		return nil, err
	}

	deposits, err := h.store.ListDepositsByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	transactions, err := h.store.ListTransactionActivityByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	withdrawals, err := h.store.ListWithdrawalsByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	awaitingExecute, err := h.store.ListPassedProposalsAwaitingExecuteByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}

	items := make([]GroupActivityItem, 0, len(deposits)+len(transactions)+len(withdrawals)+len(awaitingExecute))
	for _, deposit := range deposits {
		item := GroupActivityItem{
			ID:           deposit.ID,
			Kind:         "deposit",
			Status:       deposit.Status,
			Symbol:       "USDC",
			AmountMicros: deposit.Amount,
			CreatedAt:    deposit.CreatedAt,
		}
		if deposit.TxHash.Valid {
			item.TxHash = deposit.TxHash.String
		}
		items = append(items, item)
	}
	for _, tx := range transactions {
		items = append(items, h.activityItemFromTransaction(ctx, tx))
	}
	for _, withdrawal := range withdrawals {
		item := GroupActivityItem{
			ID:           withdrawal.ID,
			Kind:         "withdrawal",
			Status:       withdrawal.Status,
			Symbol:       "USDC",
			AmountMicros: withdrawal.Amount,
			CreatedAt:    withdrawal.CreatedAt,
		}
		if withdrawal.TxHash.Valid {
			item.TxHash = withdrawal.TxHash.String
		}
		items = append(items, item)
	}
	for _, proposal := range awaitingExecute {
		item := GroupActivityItem{
			ID:        proposal.ID,
			Kind:      string(proposal.Kind),
			Status:    postgres.TransactionStatusPending,
			CreatedAt: proposal.CreatedAt,
		}
		switch proposal.Kind {
		case domain.ProposalKindSell:
			item.Symbol = proposal.Symbol
			item.TokenAmount = proposal.TokenAmount
		case domain.ProposalKindAddAgent:
			item.AgentDisplayName = agentDisplayNameFromProposal(proposal)
			item.AmountMicros = proposal.AllocationUsdcMicros
		case domain.ProposalKindPauseAgent, domain.ProposalKindResumeAgent, domain.ProposalKindRevokeAgent:
			item.AgentDisplayName = agentDisplayNameFromProposal(proposal)
		default:
			item.Symbol = proposal.Symbol
			item.AmountMicros = proposal.UsdcMicros
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (h *HomeService) activityItemFromTransaction(ctx context.Context, tx postgres.TransactionActivityRow) GroupActivityItem {
	item := GroupActivityItem{
		ID:           tx.ID,
		Kind:         tx.Action,
		Status:       tx.Status,
		AmountMicros: tx.Amount,
		CreatedAt:    tx.CreatedAt,
		InitiatedBy:  tx.InitiatedBy,
	}
	if tx.InitiatedBy == "agent" {
		item.AgentDisplayName = tx.AgentDisplayName
	}
	switch tx.Action {
	case postgres.TransactionActionBuy:
		item.Symbol = h.symbolForMint(ctx, tx.OutputToken)
	case postgres.TransactionActionSell:
		item.Symbol = h.symbolForMint(ctx, tx.InputToken)
		item.TokenAmount = tx.Amount
		if tx.Status == postgres.TransactionStatusConfirmed && tx.CostBasisAmount.Valid {
			item.ProceedsUsdcMicros = tx.CostBasisAmount.Int64
			item.AmountMicros = tx.CostBasisAmount.Int64
		}
	}
	if tx.TxHash.Valid {
		item.TxHash = tx.TxHash.String
	}
	return item
}

func agentDisplayNameFromProposal(proposal postgres.ProposalRow) string {
	if proposal.AgentDisplayName != "" {
		return proposal.AgentDisplayName
	}
	return proposal.Symbol
}

func (h *HomeService) symbolForMint(ctx context.Context, mint string) string {
	if h.symbols != nil {
		return h.symbols.SymbolForMint(ctx, mint)
	}
	return symbolForOutputToken(ctx, nil, mint)
}
