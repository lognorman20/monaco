package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// maxNotificationWriteBodyBytes caps the read and device bodies: 200 uuids fit in 8 KiB.
const maxNotificationWriteBodyBytes = 16 << 10

// NotificationHandlers serves the inbox, push devices and proposal reminders.
type NotificationHandlers struct {
	Inbox      *app.NotificationService
	Governance *app.GovernanceService
}

type notificationResponse struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	Category        string  `json:"category"`
	Title           string  `json:"title"`
	Body            string  `json:"body"`
	GroupID         *string `json:"groupId"`
	GroupName       *string `json:"groupName"`
	GroupPictureURL *string `json:"groupPictureUrl"`
	ProposalID      *string `json:"proposalId"`
	TransactionID   *string `json:"transactionId"`
	Symbol          *string `json:"symbol"`
	ReadAt          *string `json:"readAt"`
	CreatedAt       string  `json:"createdAt"`
}

type listNotificationsResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	UnreadCount   int                    `json:"unreadCount"`
	NextCursor    string                 `json:"nextCursor,omitempty"`
}

type markNotificationsReadRequest struct {
	IDs []string `json:"ids"`
	All bool     `json:"all"`
}

type unreadCountResponse struct {
	UnreadCount int `json:"unreadCount"`
}

type registerDeviceRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
	AppEnv   string `json:"appEnv"`
}

type nudgeResponse struct {
	Reminded  int `json:"reminded"`
	WaitingOn int `json:"waitingOn"`
}

// ListNotificationsHandler handles GET /v1/me/notifications?cursor=&limit=.
func (h *NotificationHandlers) ListNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/me/notifications")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	limit := app.NotificationsDefaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}
	page, err := h.Inbox.List(ctx, token, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		writeNotificationError(ctx, log, w, err)
		return
	}
	items := make([]notificationResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toNotificationResponse(item))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(listNotificationsResponse{Notifications: items, UnreadCount: page.UnreadCount, NextCursor: page.NextCursor})
	logJSONOK(ctx, log, "ok", "count", len(items), "unread", page.UnreadCount, "has_more", page.NextCursor != "")
}

// MarkNotificationsReadHandler handles POST /v1/me/notifications/read.
func (h *NotificationHandlers) MarkNotificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/me/notifications/read")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	var req markNotificationsReadRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationWriteBodyBytes)
	if !decodeJSONBody(ctx, log, w, r, &req) {
		return
	}
	unread, err := h.Inbox.MarkRead(ctx, token, req.IDs, req.All)
	if err != nil {
		writeNotificationError(ctx, log, w, err, "ids", len(req.IDs), "all", req.All)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(unreadCountResponse{UnreadCount: unread})
	logJSONOK(ctx, log, "ok", "ids", len(req.IDs), "all", req.All, "unread", unread)
}

// RegisterDeviceHandler handles PUT /v1/me/devices.
func (h *NotificationHandlers) RegisterDeviceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "PUT /v1/me/devices")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	var req registerDeviceRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationWriteBodyBytes)
	if !decodeJSONBody(ctx, log, w, r, &req) {
		return
	}
	if err := h.Inbox.RegisterDevice(ctx, token, req.Token, req.Platform, req.AppEnv); err != nil {
		writeNotificationError(ctx, log, w, err, "platform", req.Platform, "app_env", req.AppEnv)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "registered", "platform", req.Platform, "app_env", req.AppEnv)
}

// UnregisterDeviceHandler handles DELETE /v1/me/devices/{token}.
func (h *NotificationHandlers) UnregisterDeviceHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "DELETE /v1/me/devices/{token}")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	if err := h.Inbox.UnregisterDevice(ctx, token, r.PathValue("token")); err != nil {
		writeNotificationError(ctx, log, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "unregistered")
}

// NudgeProposalHandler handles POST /v1/proposals/{id}/nudge.
func (h *NotificationHandlers) NudgeProposalHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/proposals/{id}/nudge")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	proposalID := strings.TrimSpace(r.PathValue("id"))
	result, err := h.Governance.NudgeProposal(ctx, token, proposalID)
	if err != nil {
		writeNudgeError(ctx, log, w, err, "proposal_id", proposalID)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(nudgeResponse{Reminded: result.Reminded, WaitingOn: result.WaitingOn})
	logJSONOK(ctx, log, "ok", "proposal_id", proposalID, "reminded", result.Reminded, "waiting_on", result.WaitingOn)
}

func toNotificationResponse(item app.NotificationItem) notificationResponse {
	resp := notificationResponse{
		ID:              item.ID,
		Kind:            item.Kind,
		Category:        item.Category,
		Title:           item.Title,
		Body:            item.Body,
		GroupID:         optionalString(item.GroupID),
		GroupName:       optionalString(item.GroupName),
		GroupPictureURL: optionalString(item.GroupPictureURL),
		ProposalID:      optionalString(item.ProposalID),
		TransactionID:   optionalString(item.TransactionID),
		Symbol:          optionalString(item.Symbol),
		CreatedAt:       item.CreatedAt.UTC().Format(time.RFC3339),
	}
	if item.ReadAt != nil {
		readAt := item.ReadAt.UTC().Format(time.RFC3339)
		resp.ReadAt = &readAt
	}
	return resp
}

func writeNotificationError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrInvalidNotificationCursor):
		logJSONError(ctx, log, "invalid_cursor", w, http.StatusBadRequest, "invalid cursor", attrs...)
	case errors.Is(err, app.ErrInvalidNotificationLimit):
		logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "invalid limit", attrs...)
	case errors.Is(err, app.ErrInvalidNotificationRead):
		logJSONError(ctx, log, "invalid_read", w, http.StatusBadRequest, "send ids (at most 200) or all: true", attrs...)
	case errors.Is(err, app.ErrInvalidDeviceToken):
		logJSONError(ctx, log, "invalid_device_token", w, http.StatusBadRequest, "invalid device token", attrs...)
	case errors.Is(err, app.ErrInvalidDevicePlatform):
		logJSONError(ctx, log, "invalid_platform", w, http.StatusBadRequest, "platform must be ios", attrs...)
	case errors.Is(err, app.ErrInvalidDeviceAppEnv):
		logJSONError(ctx, log, "invalid_app_env", w, http.StatusBadRequest, "appEnv must be debug or production", attrs...)
	default:
		logJSONError(ctx, log, "notifications_failed", w, http.StatusInternalServerError, "internal server error", append(attrs, "err", err.Error())...)
	}
}

func writeNudgeError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	if writeFakerReadOnly(ctx, log, w, err, attrs...) {
		return
	}
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrProposalNotFound), errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "proposal_not_found", w, http.StatusNotFound, "proposal not found", attrs...)
	case errors.Is(err, app.ErrProposalNotOpen):
		logJSONError(ctx, log, "proposal_not_open", w, http.StatusConflict, "proposal not open", attrs...)
	case errors.Is(err, app.ErrNudgeNotAllowed):
		logJSONError(ctx, log, "nudge_not_allowed", w, http.StatusForbidden, "vote first to remind the others", attrs...)
	case errors.Is(err, app.ErrRateLimited):
		seconds := 1
		var limited *app.RateLimitError
		if errors.As(err, &limited) {
			seconds = int(math.Ceil(limited.RetryAfter.Seconds()))
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		logJSONError(ctx, log, "nudge_rate_limited", w, http.StatusTooManyRequests, "the voters were reminded less than an hour ago", append(attrs, "retry_after_s", seconds)...)
	default:
		logJSONError(ctx, log, "nudge_failed", w, http.StatusInternalServerError, "internal server error", append(attrs, "err", err.Error())...)
	}
}
