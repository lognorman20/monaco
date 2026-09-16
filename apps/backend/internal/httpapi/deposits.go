package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
	TxSignature string `json:"txSignature,omitempty"`
	ShareUnits  int64  `json:"shareUnits"`
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

	var req createDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeJSONError(w, http.StatusBadRequest, "amount must be positive")
		return
	}

	result, err := h.Deposits.CreateDeposit(r.Context(), token, groupID, req.Amount)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		if errors.Is(err, app.ErrGroupNotFound) {
			writeJSONError(w, http.StatusNotFound, "group not found")
			return
		}
		if errors.Is(err, app.ErrNotGroupMember) {
			writeJSONError(w, http.StatusForbidden, "not a group member")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
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
}

// GetDepositHandler handles GET /v1/deposits/{id}.
func (h *DepositHandlers) GetDepositHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	depositID := strings.TrimSpace(r.PathValue("id"))
	if depositID == "" {
		writeJSONError(w, http.StatusBadRequest, "deposit id is required")
		return
	}

	deposit, position, err := h.Deposits.GetDeposit(r.Context(), token, depositID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrDepositNotFound) {
			writeJSONError(w, http.StatusNotFound, "deposit not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(getDepositResponse{
		DepositID:   deposit.ID,
		GroupID:     deposit.GroupID,
		Amount:      deposit.Amount,
		Status:      string(deposit.Status),
		TxSignature: deposit.TxSignature,
		ShareUnits:  position.ShareUnits,
	})
}

// GetMemberShareUnitsHandler handles GET /v1/groups/{id}/share-units.
func (h *DepositHandlers) GetMemberShareUnitsHandler(w http.ResponseWriter, r *http.Request) {
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

	position, err := h.Deposits.GetMemberPosition(r.Context(), token, groupID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(memberShareUnitsResponse{
		GroupID:    groupID,
		ShareUnits: position.ShareUnits,
	})
}

// GetTreasuryUsdcBalanceHandler handles GET /v1/groups/{id}/treasury/usdc.
func (h *DepositHandlers) GetTreasuryUsdcBalanceHandler(w http.ResponseWriter, r *http.Request) {
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

	balance, treasuryAddress, err := h.Deposits.GetTreasuryUSDCBalance(r.Context(), token, groupID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrGroupNotFound) {
			writeJSONError(w, http.StatusNotFound, "group not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(treasuryUsdcBalanceResponse{
		GroupID:         groupID,
		TreasuryAddress: treasuryAddress,
		UsdcBalance:     balance,
	})
}
