package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// AuthHandlers serves auth HTTP routes.
type AuthHandlers struct {
	Sessions *app.SessionService
}

type authSessionRequest struct {
	AccessToken string `json:"accessToken"`
}

type authSessionResponse struct {
	UserID              string `json:"userId"`
	DisplayName         string `json:"displayName"`
	MemberWalletAddress string `json:"memberWalletAddress"`
}

// SessionHandler handles POST /v1/auth/session.
func (h *AuthHandlers) SessionHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/auth/session")

	var req authSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.Sessions.OpenSession(ctx, req.AccessToken)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		logJSONError(ctx, log, "open_session_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(authSessionResponse{
		UserID:              result.UserID,
		DisplayName:         result.DisplayName,
		MemberWalletAddress: result.MemberWalletAddress,
	})
	logJSONOK(ctx, log, "session_opened", "user_id", result.UserID)
}
