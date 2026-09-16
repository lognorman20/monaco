package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// MeHandlers serves GET /v1/me.
type MeHandlers struct {
	Sessions *app.SessionService
}

type meResponse struct {
	UserID              string `json:"userId"`
	DisplayName         string `json:"displayName"`
	MemberWalletAddress string `json:"memberWalletAddress"`
}

// MeHandler handles GET /v1/me.
func (h *MeHandlers) MeHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	result, err := h.Sessions.GetMe(r.Context(), token)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired access token")
			return
		}
		if errors.Is(err, app.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{
		UserID:              result.UserID,
		DisplayName:         result.DisplayName,
		MemberWalletAddress: result.MemberWalletAddress,
	})
}

func bearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
