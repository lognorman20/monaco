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
	var req authSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.Sessions.OpenSession(r.Context(), req.AccessToken)
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
	_ = json.NewEncoder(w).Encode(authSessionResponse{
		UserID:              result.UserID,
		DisplayName:         result.DisplayName,
		MemberWalletAddress: result.MemberWalletAddress,
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
