package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// GroupActivityItem is one row in GET /v1/groups/{id}/activity.
type GroupActivityItem struct {
	ID           string
	Kind         string
	Status       string
	Symbol       string
	AmountMicros int64
	CreatedAt    time.Time
	TxSignature  string
}

// ListGroupActivity returns deposits and treasury swaps for a group member.
func (h *HomeService) ListGroupActivity(ctx context.Context, accessToken, groupID string) ([]GroupActivityItem, error) {
	if strings.TrimSpace(groupID) == "" {
		return nil, fmt.Errorf("group id is required")
	}

	user, err := h.authorizeGroupMember(ctx, accessToken, groupID)
	if err != nil {
		return nil, err
	}
	_ = user

	deposits, err := h.store.ListDepositsByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	transactions, err := h.store.ListTransactionsByGroupID(ctx, groupID)
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
		if deposit.TxSignature.Valid {
			item.TxSignature = deposit.TxSignature.String
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
		if withdrawal.TxSignature.Valid {
			item.TxSignature = withdrawal.TxSignature.String
		}
		items = append(items, item)
	}
	for _, proposal := range awaitingExecute {
		items = append(items, GroupActivityItem{
			ID:           proposal.ID,
			Kind:         postgres.TransactionActionBuy,
			Status:       postgres.TransactionStatusPending,
			Symbol:       proposal.Symbol,
			AmountMicros: proposal.UsdcMicros,
			CreatedAt:    proposal.CreatedAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (h *HomeService) authorizeGroupMember(ctx context.Context, accessToken, groupID string) (string, error) {
	identity, err := h.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}

	member, err := h.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", ErrGroupNotFound
	}
	return user.ID, nil
}

func (h *HomeService) activityItemFromTransaction(ctx context.Context, tx postgres.TransactionRow) GroupActivityItem {
	item := GroupActivityItem{
		ID:           tx.ID,
		Kind:         tx.Action,
		Status:       tx.Status,
		AmountMicros: tx.Amount,
		CreatedAt:    tx.CreatedAt,
	}
	switch tx.Action {
	case postgres.TransactionActionBuy:
		item.Symbol = h.symbolForMint(ctx, tx.OutputMint)
	case postgres.TransactionActionSell:
		if tx.Status == postgres.TransactionStatusConfirmed && tx.CostBasisAmount.Valid {
			item.Symbol = "USDC"
			item.AmountMicros = tx.CostBasisAmount.Int64
		} else {
			item.Symbol = h.symbolForMint(ctx, tx.InputMint)
		}
	}
	if tx.TxSignature.Valid {
		item.TxSignature = tx.TxSignature.String
	}
	return item
}

func (h *HomeService) symbolForMint(ctx context.Context, mint string) string {
	if h.symbols != nil {
		return h.symbols.SymbolForMint(ctx, mint)
	}
	return symbolForOutputMint(ctx, nil, mint)
}
