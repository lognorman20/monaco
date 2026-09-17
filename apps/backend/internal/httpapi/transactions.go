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

// TransactionHandlers serves transaction HTTP routes.
type TransactionHandlers struct {
	Store   *postgres.Store
	Privy   privy.Client
	XStocks xstocks.Resolver
	Swap    *app.SwapService
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

// RetryTransactionHandler handles POST /v1/transactions/{id}/retry.
func (h *TransactionHandlers) RetryTransactionHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/transactions/{id}/retry")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	transactionID := strings.TrimSpace(r.PathValue("id"))
	if transactionID == "" {
		logJSONError(ctx, log, "missing_transaction_id", w, http.StatusBadRequest, "transaction id is required")
		return
	}
	if h.Swap == nil {
		logJSONError(ctx, log, "swap_unavailable", w, http.StatusInternalServerError, "internal server error")
		return
	}

	row, found, err := h.Store.GetTransactionByID(ctx, transactionID)
	if err != nil {
		logJSONError(ctx, log, "get_transaction_failed", w, http.StatusInternalServerError, "internal server error", "transaction_id", transactionID, "err", err.Error())
		return
	}
	if !found {
		logJSONError(ctx, log, "transaction_not_found", w, http.StatusNotFound, "transaction not found", "transaction_id", transactionID)
		return
	}

	userID, err := h.authorizeGroupMemberForTransaction(ctx, token, row.GroupID)
	if err != nil {
		writeTransactionError(ctx, log, w, err, "transaction_id", transactionID, "group_id", row.GroupID)
		return
	}

	result, err := h.Swap.RetryFailedSwap(ctx, app.RetryFailedSwapRequest{
		TransactionID: transactionID,
		UserID:        userID,
	})
	if err != nil {
		switch {
		case errors.Is(err, app.ErrTransactionNotRetryable):
			logJSONError(ctx, log, "transaction_not_retryable", w, http.StatusConflict, "transaction not retryable", "transaction_id", transactionID)
		case errors.Is(err, app.ErrTransactionNotFound):
			logJSONError(ctx, log, "transaction_not_found", w, http.StatusNotFound, "transaction not found", "transaction_id", transactionID)
		default:
			logJSONError(ctx, log, "retry_transaction_failed", w, http.StatusInternalServerError, "internal server error", "transaction_id", transactionID, "err", err.Error())
		}
		return
	}

	resp := getTransactionResponse{
		TransactionID: result.Transaction.ID,
		GroupID:       result.Transaction.GroupID,
		Action:        result.Transaction.Action,
		Status:        result.Transaction.Status,
	}
	if result.Transaction.TxSignature.Valid {
		resp.TxSignature = result.Transaction.TxSignature.String
	}
	if result.Transaction.CostBasisPrice.Valid {
		resp.CostBasisPrice = result.Transaction.CostBasisPrice.Int64
	}
	if result.Transaction.CostBasisAmount.Valid {
		resp.CostBasisAmount = result.Transaction.CostBasisAmount.Int64
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
	logJSONOK(ctx, log, "ok",
		"transaction_id", result.Transaction.ID,
		"group_id", result.Transaction.GroupID,
		"status", result.Transaction.Status,
		"created", result.Created,
	)
}

// GetTransactionHandler handles GET /v1/transactions/{id}.
func (h *TransactionHandlers) GetTransactionHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/transactions/{id}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	transactionID := strings.TrimSpace(r.PathValue("id"))
	if transactionID == "" {
		logJSONError(ctx, log, "missing_transaction_id", w, http.StatusBadRequest, "transaction id is required")
		return
	}

	row, err := h.getTransactionForMember(ctx, token, transactionID)
	if err != nil {
		writeTransactionError(ctx, log, w, err, "transaction_id", transactionID)
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
	logJSONOK(ctx, log, "ok",
		"transaction_id", row.ID,
		"group_id", row.GroupID,
		"status", row.Status,
	)
}

// GetTreasuryTokenBalancesHandler handles GET /v1/groups/{id}/treasury/tokens.
func (h *TransactionHandlers) GetTreasuryTokenBalancesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/treasury/tokens")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusBadRequest, "group id is required")
		return
	}

	treasuryAddress, err := h.authorizeGroupMember(ctx, token, groupID)
	if err != nil {
		writeTransactionError(ctx, log, w, err, "group_id", groupID)
		return
	}

	usdcBalance, err := h.Privy.TreasuryUSDCBalance(ctx, treasuryAddress)
	if err != nil {
		logJSONError(ctx, log, "treasury_usdc_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	holdings, err := h.Store.ListNetTokenHoldingsByGroup(ctx, groupID)
	if err != nil {
		logJSONError(ctx, log, "list_holdings_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
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
	logJSONOK(ctx, log, "ok", "group_id", groupID, "token_count", len(tokens), "usdc_balance", usdcBalance)
}

// GetCostBasisBySymbolHandler handles GET /v1/groups/{id}/cost-basis/{symbol}.
func (h *TransactionHandlers) GetCostBasisBySymbolHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/cost-basis/{symbol}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	symbol := strings.TrimSpace(r.PathValue("symbol"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusBadRequest, "group id is required")
		return
	}
	if symbol == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusBadRequest, "symbol is required", "group_id", groupID)
		return
	}

	if _, err := h.authorizeGroupMember(ctx, token, groupID); err != nil {
		writeTransactionError(ctx, log, w, err, "group_id", groupID, "symbol", symbol)
		return
	}

	outputMint, err := h.XStocks.ResolveSolanaMint(ctx, symbol)
	if err != nil {
		logJSONError(ctx, log, "symbol_not_found", w, http.StatusNotFound, "symbol not found", "group_id", groupID, "symbol", symbol)
		return
	}

	price, amount, found, err := h.Store.GetFillDerivedCostBasisByOutputMint(ctx, groupID, outputMint)
	if err != nil {
		logJSONError(ctx, log, "cost_basis_lookup_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "symbol", symbol, "err", err.Error())
		return
	}
	if !found {
		logJSONError(ctx, log, "cost_basis_not_found", w, http.StatusNotFound, "cost basis not found", "group_id", groupID, "symbol", symbol)
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
	logJSONOK(ctx, log, "ok", "group_id", groupID, "symbol", symbol)
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

func (h *TransactionHandlers) authorizeGroupMemberForTransaction(ctx context.Context, accessToken, groupID string) (string, error) {
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

	member, err := h.Store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if !member {
		return "", app.ErrGroupNotFound
	}
	return user.ID, nil
}

var errTransactionNotFound = errors.New("transaction not found")

func writeTransactionError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", attrs...)
	case errors.Is(err, errTransactionNotFound):
		logJSONError(ctx, log, "transaction_not_found", w, http.StatusNotFound, "transaction not found", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "internal_error", w, http.StatusInternalServerError, "internal server error", all...)
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
