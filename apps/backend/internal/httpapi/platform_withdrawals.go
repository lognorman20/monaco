package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// PlatformWithdrawHandlers serves platform withdrawal HTTP routes.
type PlatformWithdrawHandlers struct {
	Withdrawals *app.PlatformWithdrawService
}

type createPlatformWithdrawalRequest struct {
	Amount    int64  `json:"amount"`
	ToAddress string `json:"toAddress"`
}

type platformWithdrawalResponse struct {
	WithdrawalID string `json:"withdrawalId"`
	Amount       int64  `json:"amount"`
	ToAddress    string `json:"toAddress"`
	Status       string `json:"status"`
	TxHash  string `json:"txHash,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// CreatePlatformWithdrawalHandler handles POST /v1/me/withdrawals.
func (h *PlatformWithdrawHandlers) CreatePlatformWithdrawalHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/me/withdrawals")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	var req createPlatformWithdrawalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		logJSONError(ctx, log, "invalid_amount", w, http.StatusBadRequest, "amount must be positive")
		return
	}

	result, err := h.Withdrawals.CreatePlatformWithdrawal(ctx, token, req.Amount, req.ToAddress)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, app.ErrInsufficientPlatformBalance) {
			logJSONError(ctx, log, "insufficient_balance", w, http.StatusBadRequest, "amount exceeds available platform balance")
			return
		}
		if errors.Is(err, app.ErrInvalidWithdrawAddress) {
			logJSONError(ctx, log, "invalid_address", w, http.StatusBadRequest, "invalid destination address")
			return
		}
		if errors.Is(err, app.ErrWithdrawToMemberWallet) {
			logJSONError(ctx, log, "member_wallet_destination", w, http.StatusBadRequest, "cannot withdraw to your deposit address")
			return
		}
		if errors.Is(err, app.ErrPendingPlatformWithdrawal) {
			logJSONError(ctx, log, "pending_withdrawal", w, http.StatusConflict, "a platform withdrawal is already in progress")
			return
		}
		logJSONError(ctx, log, "create_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(platformWithdrawalResponseFromApp(result))
	logJSONOK(ctx, log, "ok",
		"withdrawal_id", result.ID,
		"amount", result.Amount,
		"to_address", result.ToAddress,
		"tx_signature", result.TxHash,
	)
}

// GetPlatformWithdrawalHandler handles GET /v1/me/withdrawals/{id}.
func (h *PlatformWithdrawHandlers) GetPlatformWithdrawalHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/withdrawals/{id}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	withdrawalID := strings.TrimSpace(r.PathValue("id"))
	if withdrawalID == "" {
		logJSONError(ctx, log, "missing_withdrawal_id", w, http.StatusBadRequest, "withdrawal id is required")
		return
	}

	result, err := h.Withdrawals.GetPlatformWithdrawal(ctx, token, withdrawalID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "withdrawal_id", withdrawalID)
			return
		}
		if errors.Is(err, app.ErrPlatformWithdrawalNotFound) {
			logJSONError(ctx, log, "not_found", w, http.StatusNotFound, "withdrawal not found", "withdrawal_id", withdrawalID)
			return
		}
		logJSONError(ctx, log, "get_failed", w, http.StatusInternalServerError, "internal server error", "withdrawal_id", withdrawalID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(platformWithdrawalResponseFromApp(result))
	logJSONOK(ctx, log, "ok", "withdrawal_id", result.ID, "status", result.Status)
}

func platformWithdrawalResponseFromApp(withdrawal app.PlatformWithdrawal) platformWithdrawalResponse {
	return platformWithdrawalResponse{
		WithdrawalID: withdrawal.ID,
		Amount:       withdrawal.Amount,
		ToAddress:    withdrawal.ToAddress,
		Status:       string(withdrawal.Status),
		TxHash:  withdrawal.TxHash,
		CreatedAt:    withdrawal.CreatedAt.UTC().Format(time.RFC3339),
	}
}
