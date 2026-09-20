package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
)

// groupMessageTimeLayout is fixed-width RFC3339 with microseconds in UTC, so clients can
// order messages within the same second and compare timestamps as strings.
const groupMessageTimeLayout = "2006-01-02T15:04:05.000000Z07:00"

// maxGroupMessageRequestBytes caps POST bodies well above the 2000-character message limit
// (4 bytes per rune plus JSON overhead) so oversized payloads fail before decode.
const maxGroupMessageRequestBytes = 16 << 10

// GroupMessageHandlers serves cabal chat routes.
type GroupMessageHandlers struct {
	Chat *app.GroupChatService
}

type postGroupMessageRequest struct {
	Body string `json:"body"`
}

type groupMessageResponse struct {
	ID         string `json:"id"`
	GroupID    string `json:"groupId"`
	AuthorID   string `json:"authorId"`
	AuthorName string `json:"authorName"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
	Mine       bool   `json:"mine"`
}

type listGroupMessagesResponse struct {
	Messages   []groupMessageResponse `json:"messages"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}

// ListGroupMessagesHandler handles GET /v1/groups/{id}/messages?before=&limit=.
func (h *GroupMessageHandlers) ListGroupMessagesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/messages")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	limit := app.GroupMessagesDefaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "invalid limit", "group_id", groupID)
			return
		}
		limit = parsed
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("before"))

	page, err := h.Chat.ListMessages(ctx, token, groupID, cursor, limit)
	if err != nil {
		writeGroupMessageError(ctx, log, w, err, "group_id", groupID)
		return
	}

	items := make([]groupMessageResponse, 0, len(page.Messages))
	for _, m := range page.Messages {
		items = append(items, toGroupMessageResponse(m))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(listGroupMessagesResponse{Messages: items, NextCursor: page.NextCursor})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "count", len(items), "has_more", page.NextCursor != "")
}

// PostGroupMessageHandler handles POST /v1/groups/{id}/messages.
func (h *GroupMessageHandlers) PostGroupMessageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/messages")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	groupID := strings.TrimSpace(r.PathValue("id"))
	if groupID == "" {
		logJSONError(ctx, log, "missing_group_id", w, http.StatusNotFound, "group not found")
		return
	}

	var req postGroupMessageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGroupMessageRequestBytes)).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			logJSONError(ctx, log, "message_too_long", w, http.StatusBadRequest, "message is too long", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", "group_id", groupID)
		return
	}

	message, err := h.Chat.PostMessage(ctx, token, groupID, req.Body)
	if err != nil {
		writeGroupMessageError(ctx, log, w, err, "group_id", groupID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toGroupMessageResponse(message))
	log.done(ctx, "ok", http.StatusCreated, "group_id", groupID, "user_id", message.AuthorID, "message_id", message.ID)
}

func toGroupMessageResponse(m app.GroupMessage) groupMessageResponse {
	return groupMessageResponse{
		ID:         m.ID,
		GroupID:    m.GroupID,
		AuthorID:   m.AuthorID,
		AuthorName: m.AuthorName,
		Body:       m.Body,
		CreatedAt:  m.CreatedAt.UTC().Format(groupMessageTimeLayout),
		Mine:       m.Mine,
	}
}

func writeGroupMessageError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	var limited *app.RateLimitedError
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", attrs...)
	case errors.Is(err, app.ErrNotGroupMember):
		logJSONError(ctx, log, "not_group_member", w, http.StatusForbidden, "not a group member", attrs...)
	case errors.Is(err, app.ErrMessageEmpty):
		logJSONError(ctx, log, "message_empty", w, http.StatusBadRequest, "message is empty", attrs...)
	case errors.Is(err, app.ErrMessageTooLong):
		logJSONError(ctx, log, "message_too_long", w, http.StatusBadRequest, "message is too long", attrs...)
	case errors.Is(err, app.ErrInvalidMessageCursor):
		logJSONError(ctx, log, "invalid_cursor", w, http.StatusBadRequest, "invalid cursor", attrs...)
	case errors.Is(err, app.ErrInvalidMessageLimit):
		logJSONError(ctx, log, "invalid_limit", w, http.StatusBadRequest, "invalid limit", attrs...)
	case errors.As(err, &limited):
		seconds := int(math.Ceil(limited.RetryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		logJSONError(ctx, log, "rate_limited", w, http.StatusTooManyRequests, "too many messages, try again shortly", append(attrs, "retry_after_s", seconds)...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "group_messages_failed", w, http.StatusInternalServerError, "internal server error", all...)
	}
}
