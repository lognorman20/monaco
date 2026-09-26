package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// maxPreferencesBodyBytes bounds PATCH /v1/me/preferences. The whole document is four
// booleans under one key; 4 KiB is framing and headroom, like PATCH /v1/me.
const maxPreferencesBodyBytes = 4 << 10

// AccountHandlers serves the member's settings and account: preferences, the deletion
// check, and DELETE /v1/me.
type AccountHandlers struct {
	Accounts *app.AccountService
}

// preferencesResponse is GET and PATCH /v1/me/preferences: every key, stored over defaults.
type preferencesResponse struct {
	Notifications notificationPreferencesResponse `json:"notifications"`
}

type notificationPreferencesResponse struct {
	Proposals bool `json:"proposals"`
	Results   bool `json:"results"`
	Chat      bool `json:"chat"`
	Money     bool `json:"money"`
}

// deletionBlockerResponse is one row of GET /v1/me/deletion-check. groupId and groupName are
// present for the cabal kinds; valueUsd is null when a slice could not be priced right now.
type deletionBlockerResponse struct {
	Kind      string  `json:"kind"`
	GroupID   string  `json:"groupId,omitempty"`
	GroupName string  `json:"groupName,omitempty"`
	ValueUsd  *string `json:"valueUsd"`
}

type deletionCheckResponse struct {
	CanDelete bool                      `json:"canDelete"`
	Blockers  []deletionBlockerResponse `json:"blockers"`
}

// deletionBlockedResponse is DELETE /v1/me refused: the error shape plus the same blockers
// the check returns, so the app can show them without asking again.
type deletionBlockedResponse struct {
	Error     string                    `json:"error"`
	RequestID string                    `json:"requestId,omitempty"`
	Reason    string                    `json:"reason"`
	CanDelete bool                      `json:"canDelete"`
	Blockers  []deletionBlockerResponse `json:"blockers"`
}

type deletedAccountResponse struct {
	DeletedAt string `json:"deletedAt"`
}

// GetPreferencesHandler handles GET /v1/me/preferences.
func (h *AccountHandlers) GetPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/preferences")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	prefs, err := h.Accounts.GetPreferences(ctx, token)
	if err != nil {
		writeAccountError(ctx, log, w, err)
		return
	}
	writeAccountJSON(w, http.StatusOK, preferencesResponseFrom(prefs))
	logJSONOK(ctx, log, "ok")
}

// PatchPreferencesHandler handles PATCH /v1/me/preferences: a JSON merge patch of known keys
// and boolean values. The response is the whole merged document.
func (h *AccountHandlers) PatchPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "PATCH /v1/me/preferences")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPreferencesBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBodyDecodeError(ctx, log, w, err)
		return
	}
	// One JSON value and nothing after it; the service checks the value itself.
	if !json.Valid(body) {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body")
		return
	}

	prefs, err := h.Accounts.UpdatePreferences(ctx, token, body)
	if err != nil {
		var prefsErr *app.PreferencesError
		if errors.As(err, &prefsErr) {
			logJSONErrorWithReason(ctx, log, "invalid_preferences", w, http.StatusBadRequest, prefsErr.Message, "invalid_preferences")
			return
		}
		writeAccountError(ctx, log, w, err)
		return
	}
	writeAccountJSON(w, http.StatusOK, preferencesResponseFrom(prefs))
	logJSONOK(ctx, log, "updated")
}

// DeletionCheckHandler handles GET /v1/me/deletion-check.
func (h *AccountHandlers) DeletionCheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/deletion-check")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	check, err := h.Accounts.DeletionCheck(ctx, token)
	if err != nil {
		writeAccountError(ctx, log, w, err)
		return
	}
	writeAccountJSON(w, http.StatusOK, deletionCheckResponse{
		CanDelete: check.CanDelete(),
		Blockers:  deletionBlockersResponse(check.Blockers),
	})
	logJSONOK(ctx, log, "ok", "can_delete", check.CanDelete(), "blockers", len(check.Blockers))
}

// DeleteMeHandler handles DELETE /v1/me.
func (h *AccountHandlers) DeleteMeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "DELETE /v1/me")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	deleted, err := h.Accounts.DeleteAccount(ctx, token)
	if err != nil {
		var blocked *app.AccountDeletionBlockedError
		if errors.As(err, &blocked) {
			log.done(ctx, "account_not_empty", http.StatusConflict, "reason", "account_not_empty", "blockers", len(blocked.Check.Blockers))
			writeAccountJSON(w, http.StatusConflict, deletionBlockedResponse{
				Error:     "Move your money out before deleting your account.",
				RequestID: RequestIDFromContext(ctx),
				Reason:    "account_not_empty",
				CanDelete: false,
				Blockers:  deletionBlockersResponse(blocked.Check.Blockers),
			})
			return
		}
		writeAccountError(ctx, log, w, err)
		return
	}
	writeAccountJSON(w, http.StatusOK, deletedAccountResponse{DeletedAt: deleted.DeletedAt.UTC().Format(time.RFC3339)})
	logJSONOK(ctx, log, "deleted", "user_id", deleted.UserID)
}

func writeAccountJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAccountError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
	case errors.Is(err, app.ErrRateLimited):
		writeRateLimited(ctx, log, w, err)
	case errors.Is(err, app.ErrBalanceUnavailable):
		logJSONError(ctx, log, "balance_unavailable", w, http.StatusServiceUnavailable, "could not check your account balance, try again shortly", "err", err.Error())
	default:
		logJSONError(ctx, log, "account_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
	}
}

func preferencesResponseFrom(prefs app.Preferences) preferencesResponse {
	return preferencesResponse{Notifications: notificationPreferencesResponse{
		Proposals: prefs.Notifications.Proposals,
		Results:   prefs.Notifications.Results,
		Chat:      prefs.Notifications.Chat,
		Money:     prefs.Notifications.Money,
	}}
}

func deletionBlockersResponse(blockers []app.DeletionBlocker) []deletionBlockerResponse {
	rows := make([]deletionBlockerResponse, 0, len(blockers))
	for _, blocker := range blockers {
		row := deletionBlockerResponse{
			Kind:      string(blocker.Kind),
			GroupID:   blocker.GroupID,
			GroupName: blocker.GroupName,
		}
		if blocker.ValueKnown {
			value := formatUsdcMicros(blocker.ValueMicros)
			row.ValueUsd = &value
		}
		rows = append(rows, row)
	}
	return rows
}
