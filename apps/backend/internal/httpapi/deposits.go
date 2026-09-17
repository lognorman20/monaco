package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// DepositHandlers serves deposit HTTP routes.
type DepositHandlers struct {
	Deposits *app.DepositService
}

type createDepositRequest struct {
	Amount int64 `json:"amount"`
}

type createDepositResponse struct {
	DepositID   string `json:"depositId"`
	GroupID     string `json:"groupId"`
	Amount      int64  `json:"amount"`
	Status      string `json:"status"`
	FromAddress string `json:"fromAddress"`
}

type getDepositResponse struct {
	DepositID   string `json:"depositId"`
	GroupID     string `json:"groupId"`
	Amount      int64  `json:"amount"`
	Status      string `json:"status"`
	FromAddress string `json:"fromAddress,omitempty"`
	TxSignature string `json:"txSignature,omitempty"`
	ShareUnits  int64  `json:"shareUnits"`
	CreatedAt   string `json:"createdAt"`
}

type memberShareUnitsResponse struct {
	GroupID    string `json:"groupId"`
	ShareUnits int64  `json:"shareUnits"`
}

type treasuryUsdcBalanceResponse struct {
	GroupID         string `json:"groupId"`
	TreasuryAddress string `json:"treasuryAddress"`
	UsdcBalance     int64  `json:"usdcBalance"`
}

// CreateDepositHandler handles POST /v1/groups/{id}/deposits.
func (h *DepositHandlers) CreateDepositHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/deposits")

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

	var req createDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", "group_id", groupID)
		return
	}
	if req.Amount <= 0 {
		logJSONError(ctx, log, "invalid_amount", w, http.StatusBadRequest, "amount must be positive", "group_id", groupID)
		return
	}

	result, err := h.Deposits.CreateDeposit(ctx, token, groupID, req.Amount)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrNotGroupMember) {
			logJSONError(ctx, log, "not_group_member", w, http.StatusForbidden, "not a group member", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "create_deposit_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(createDepositResponse{
		DepositID:   result.Deposit.ID,
		GroupID:     result.Deposit.GroupID,
		Amount:      result.Deposit.Amount,
		Status:      string(result.Deposit.Status),
		FromAddress: result.Deposit.FromAddress,
	})
	logJSONOK(ctx, log, "deposit_created",
		"deposit_id", result.Deposit.ID,
		"group_id", result.Deposit.GroupID,
		"amount", result.Deposit.Amount,
	)
}

// GetDepositHandler handles GET /v1/deposits/{id}.
func (h *DepositHandlers) GetDepositHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/deposits/{id}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	depositID := strings.TrimSpace(r.PathValue("id"))
	if depositID == "" {
		logJSONError(ctx, log, "missing_deposit_id", w, http.StatusBadRequest, "deposit id is required")
		return
	}

	deposit, position, err := h.Deposits.GetDeposit(ctx, token, depositID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "deposit_id", depositID)
			return
		}
		if errors.Is(err, app.ErrDepositNotFound) {
			logJSONError(ctx, log, "deposit_not_found", w, http.StatusNotFound, "deposit not found", "deposit_id", depositID)
			return
		}
		logJSONError(ctx, log, "get_deposit_failed", w, http.StatusInternalServerError, "internal server error", "deposit_id", depositID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(getDepositResponse{
		DepositID:   deposit.ID,
		GroupID:     deposit.GroupID,
		Amount:      deposit.Amount,
		Status:      string(deposit.Status),
		FromAddress: deposit.FromAddress,
		TxSignature: deposit.TxSignature,
		ShareUnits:  position.ShareUnits,
		CreatedAt:   deposit.CreatedAt.UTC().Format(time.RFC3339),
	})
	logJSONOK(ctx, log, "ok", "deposit_id", deposit.ID, "group_id", deposit.GroupID, "status", deposit.Status)
}

// GetMemberShareUnitsHandler handles GET /v1/groups/{id}/share-units.
func (h *DepositHandlers) GetMemberShareUnitsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/share-units")

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

	position, err := h.Deposits.GetMemberPosition(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "get_share_units_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(memberShareUnitsResponse{
		GroupID:    groupID,
		ShareUnits: position.ShareUnits,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "share_units", position.ShareUnits)
}

// GetTreasuryUsdcBalanceHandler handles GET /v1/groups/{id}/treasury/usdc.
func (h *DepositHandlers) GetTreasuryUsdcBalanceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/treasury/usdc")

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

	balance, treasuryAddress, err := h.Deposits.GetTreasuryUSDCBalance(ctx, token, groupID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "treasury_balance_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(treasuryUsdcBalanceResponse{
		GroupID:         groupID,
		TreasuryAddress: treasuryAddress,
		UsdcBalance:     balance,
	})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "usdc_balance", balance)
}
