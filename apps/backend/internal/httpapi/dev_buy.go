package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// DevBuyHandlers serves the temporary M3 dev-only buy route.
type DevBuyHandlers struct {
	DevBuy *app.DevBuyService
}

type devBuyRequest struct {
	Symbol string `json:"symbol"`
	USDC   int64  `json:"usdc"`
}

type devBuyResponse struct {
	TransactionID string `json:"transactionId"`
	GroupID       string `json:"groupId"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	TxSignature   string `json:"txSignature,omitempty"`
	Created       bool   `json:"created"`
}

// DevBuyHandler handles POST /v1/dev/groups/{id}/buy.
func (h *DevBuyHandlers) DevBuyHandler(w http.ResponseWriter, r *http.Request) {
	if !app.DevBuyEnabled() {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		writeJSONError(w, http.StatusNotFound, "group not found")
		return
	}

	var req devBuyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Symbol) == "" {
		writeJSONError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if req.USDC <= 0 {
		writeJSONError(w, http.StatusBadRequest, "usdc must be positive")
		return
	}

	result, err := h.DevBuy.ExecuteDevBuy(r.Context(), token, app.DevBuyRequest{
		GroupID: groupID,
		Symbol:  req.Symbol,
		USDC:    req.USDC,
	})
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrNotGroupMember) {
			writeJSONError(w, http.StatusForbidden, "not a group member")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			writeJSONError(w, http.StatusNotFound, "group not found")
			return
		}
		if errors.Is(err, xstocks.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "symbol not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(devBuyResponse{
		TransactionID: result.TransactionID,
		GroupID:       result.GroupID,
		Symbol:        result.Symbol,
		Status:        result.Status,
		TxSignature:   result.TxSignature,
		Created:       result.Created,
	})
}
