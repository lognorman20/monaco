package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// TransactionHandlers serves transaction read HTTP routes.
type TransactionHandlers struct {
	Store   *postgres.Store
	Privy   privy.Client
	XStocks xstocks.Resolver
}

type getTransactionResponse struct {
	TransactionID   string `json:"transactionId"`
	GroupID         string `json:"groupId"`
	Action          string `json:"action"`
	Status          string `json:"status"`
	TxSignature     string `json:"txSignature,omitempty"`
	CostBasisPrice  int64  `json:"costBasisPrice,omitempty"`
	CostBasisAmount int64  `json:"costBasisAmount,omitempty"`
}

type treasuryTokenBalance struct {
	Symbol string `json:"symbol,omitempty"`
	Mint   string `json:"mint"`
	Amount int64  `json:"amount"`
}

type treasuryTokenBalancesResponse struct {
	GroupID         string                 `json:"groupId"`
	TreasuryAddress string                 `json:"treasuryAddress"`
	UsdcBalance     int64                  `json:"usdcBalance"`
	Tokens          []treasuryTokenBalance `json:"tokens"`
}

type costBasisBySymbolResponse struct {
	GroupID         string `json:"groupId"`
	Symbol          string `json:"symbol"`
	CostBasisPrice  int64  `json:"costBasisPrice"`
	CostBasisAmount int64  `json:"costBasisAmount"`
}

// GetTransactionHandler handles GET /v1/transactions/{id}.
func (h *TransactionHandlers) GetTransactionHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	transactionID := strings.TrimSpace(r.PathValue("id"))
	if transactionID == "" {
		writeJSONError(w, http.StatusBadRequest, "transaction id is required")
		return
	}

	row, err := h.getTransactionForMember(r.Context(), token, transactionID)
	if err != nil {
		writeTransactionError(w, err)
		return
	}

	resp := getTransactionResponse{
		TransactionID: row.ID,
		GroupID:       row.GroupID,
		Action:        row.Action,
		Status:        row.Status,
	}
	if row.TxSignature.Valid {
		resp.TxSignature = row.TxSignature.String
	}
	if row.CostBasisPrice.Valid {
		resp.CostBasisPrice = row.CostBasisPrice.Int64
	}
	if row.CostBasisAmount.Valid {
		resp.CostBasisAmount = row.CostBasisAmount.Int64
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GetTreasuryTokenBalancesHandler handles GET /v1/groups/{id}/treasury/tokens.
func (h *TransactionHandlers) GetTreasuryTokenBalancesHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		writeJSONError(w, http.StatusBadRequest, "group id is required")
		return
	}

	treasuryAddress, err := h.authorizeGroupMember(r.Context(), token, groupID)
	if err != nil {
		writeTransactionError(w, err)
		return
	}

	usdcBalance, err := h.Privy.TreasuryUSDCBalance(r.Context(), treasuryAddress)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	holdings, err := h.Store.ListNetTokenHoldingsByGroup(r.Context(), groupID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	tokens := make([]treasuryTokenBalance, 0, len(holdings))
	for _, holding := range holdings {
		tokens = append(tokens, treasuryTokenBalance{
			Symbol: symbolForMint(holding.Mint),
			Mint:   holding.Mint,
			Amount: holding.Amount,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(treasuryTokenBalancesResponse{
		GroupID:         groupID,
		TreasuryAddress: treasuryAddress,
		UsdcBalance:     usdcBalance,
		Tokens:          tokens,
	})
}

// GetCostBasisBySymbolHandler handles GET /v1/groups/{id}/cost-basis/{symbol}.
func (h *TransactionHandlers) GetCostBasisBySymbolHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if groupID == "" {
		writeJSONError(w, http.StatusBadRequest, "group id is required")
		return
	}
	if symbol == "" {
		writeJSONError(w, http.StatusBadRequest, "symbol is required")
		return
	}

	if _, err := h.authorizeGroupMember(r.Context(), token, groupID); err != nil {
		writeTransactionError(w, err)
		return
	}

	outputMint, err := h.XStocks.ResolveSolanaMint(r.Context(), symbol)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "symbol not found")
		return
	}

	price, amount, found, err := h.Store.GetFillDerivedCostBasisByOutputMint(r.Context(), groupID, outputMint)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "cost basis not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(costBasisBySymbolResponse{
		GroupID:         groupID,
		Symbol:          symbol,
		CostBasisPrice:  price,
		CostBasisAmount: amount,
	})
}

func (h *TransactionHandlers) getTransactionForMember(ctx context.Context, accessToken, transactionID string) (postgres.TransactionRow, error) {
	row, found, err := h.Store.GetTransactionByID(ctx, transactionID)
	if err != nil {
		return postgres.TransactionRow{}, err
	}
	if !found {
		return postgres.TransactionRow{}, errTransactionNotFound
	}

	if _, err := h.authorizeGroupMember(ctx, accessToken, row.GroupID); err != nil {
		return postgres.TransactionRow{}, err
	}
	return row, nil
}

func (h *TransactionHandlers) authorizeGroupMember(ctx context.Context, accessToken, groupID string) (string, error) {
	identity, err := h.Privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.Store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrUserNotFound
	}

	group, found, err := h.Store.GetGroupByID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found || group.CreatorUserID != user.ID {
		return "", app.ErrGroupNotFound
	}

	treasury, found, err := h.Store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrGroupNotFound
	}
	return treasury.SolanaAddress, nil
}

var errTransactionNotFound = errors.New("transaction not found")

func writeTransactionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, app.ErrGroupNotFound):
		writeJSONError(w, http.StatusNotFound, "group not found")
	case errors.Is(err, errTransactionNotFound):
		writeJSONError(w, http.StatusNotFound, "transaction not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
	}
}

func symbolForMint(mint string) string {
	switch mint {
	case jupiter.AAPLxMint:
		return "AAPLx"
	default:
		return ""
	}
}
