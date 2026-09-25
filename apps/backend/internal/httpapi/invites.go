package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
)

// InviteHandlers serves the invite-code routes:
//
//	GET  /v1/groups/{id}/invites          the live code, created if the cabal has none
//	POST /v1/groups/{id}/invites          a new code; the old one stops working
//	POST /v1/groups/{id}/invites/revoke   no live code until a member asks again
//	GET  /v1/invites/{code}               public preview, no session, limited per IP
//	POST /v1/groups/join-by-code          join (or ask to join) through a code
type InviteHandlers struct {
	Invites *app.InviteService
}

type inviteResponse struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

type invitePreviewResponse struct {
	Code        string  `json:"code"`
	GroupID     string  `json:"groupId"`
	Name        string  `json:"name"`
	MemberCount int     `json:"memberCount"`
	Tint        string  `json:"tint"`
	PictureURL  *string `json:"pictureUrl"`
	JoinPolicy  string  `json:"joinPolicy"`
	PotValueUsd string  `json:"potValueUsd"`
}

type joinByCodeRequest struct {
	Code string `json:"code"`
}

// joinByCodePendingResponse is the join route's 202 body plus the cabal the code named.
type joinByCodePendingResponse struct {
	Status  string `json:"status"`
	GroupID string `json:"groupId"`
}

// GetGroupInviteHandler handles GET /v1/groups/{id}/invites.
func (h *InviteHandlers) GetGroupInviteHandler(w http.ResponseWriter, r *http.Request) {
	h.serveGroupInvite(w, r, "GET /v1/groups/{id}/invites", http.StatusOK, h.Invites.CurrentInvite)
}

// CreateGroupInviteHandler handles POST /v1/groups/{id}/invites.
func (h *InviteHandlers) CreateGroupInviteHandler(w http.ResponseWriter, r *http.Request) {
	h.serveGroupInvite(w, r, "POST /v1/groups/{id}/invites", http.StatusCreated, h.Invites.NewInvite)
}

func (h *InviteHandlers) serveGroupInvite(
	w http.ResponseWriter,
	r *http.Request,
	route string,
	status int,
	load func(ctx context.Context, accessToken, groupID string) (app.Invite, error),
) {
	ctx := r.Context()
	log := newRequestLog(r, route)
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	invite, err := load(ctx, token, groupID)
	if err != nil {
		writeInviteGroupError(ctx, log, w, err, groupID)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(inviteResponse{Code: invite.Code, URL: invite.URL})
	log.done(ctx, "ok", status, "group_id", groupID)
}

// RevokeGroupInviteHandler handles POST /v1/groups/{id}/invites/revoke.
func (h *InviteHandlers) RevokeGroupInviteHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/invites/revoke")
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	groupID := r.PathValue("id")
	if err := h.Invites.RevokeInvite(ctx, token, groupID); err != nil {
		writeInviteGroupError(ctx, log, w, err, groupID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logNoContent(ctx, log, "revoked", "group_id", groupID)
}

func writeInviteGroupError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, groupID string) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
	case errors.Is(err, app.ErrUserNotFound), errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
	default:
		logJSONError(ctx, log, "invite_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
	}
}

// GetInvitePreviewHandler handles GET /v1/invites/{code}. No session: the landing page and
// a signed-out phone both read it. The rate limiter's public class caps it per IP.
func (h *InviteHandlers) GetInvitePreviewHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/invites/{code}")
	preview, err := h.Invites.Preview(ctx, r.PathValue("code"))
	if err != nil {
		if errors.Is(err, app.ErrInviteNotFound) {
			logJSONError(ctx, log, "invite_not_found", w, http.StatusNotFound, "invite not found")
			return
		}
		logJSONError(ctx, log, "invite_preview_failed", w, http.StatusInternalServerError, "internal server error", "err", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// A revoked code must stop previewing at once, so nothing may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(invitePreviewResponse{
		Code:        preview.Code,
		GroupID:     preview.GroupID,
		Name:        preview.Name,
		MemberCount: preview.MemberCount,
		Tint:        preview.Tint,
		PictureURL:  optionalString(preview.PictureURL),
		JoinPolicy:  string(preview.JoinPolicy),
		PotValueUsd: preview.PotValueUsd,
	})
	logJSONOK(ctx, log, "ok", "group_id", preview.GroupID)
}

// JoinByCodeHandler handles POST /v1/groups/join-by-code. It answers as
// POST /v1/groups/{id}/join does, 204 when the member is in and 202 {"status":"pending"}
// when the cabal's admin approves members, and adds which cabal the code named: `groupId`
// in the 202 body and a `Location` header on both.
func (h *InviteHandlers) JoinByCodeHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/join-by-code")
	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	var req joinByCodeRequest
	if !decodeJSONBody(ctx, log, w, r, &req) {
		return
	}
	groupID, outcome, err := h.Invites.JoinByCode(ctx, token, req.Code)
	if err != nil {
		if writeFakerReadOnly(ctx, log, w, err, "group_id", groupID) {
			return
		}
		switch {
		case errors.Is(err, privy.ErrInvalidToken):
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token")
		case errors.Is(err, app.ErrInviteNotFound), errors.Is(err, app.ErrGroupNotFound):
			logJSONError(ctx, log, "invite_not_found", w, http.StatusNotFound, "invite not found", "group_id", groupID)
		case errors.Is(err, app.ErrUserNotFound):
			logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found")
		default:
			logJSONError(ctx, log, "join_by_code_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		}
		return
	}
	switch outcome {
	case app.JoinOutcomePending:
		w.Header().Set("Location", "/v1/groups/"+groupID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(joinByCodePendingResponse{Status: string(outcome), GroupID: groupID})
		log.done(ctx, "join_pending", http.StatusAccepted, "group_id", groupID)
	case app.JoinOutcomeJoined, app.JoinOutcomeAlreadyMember:
		w.Header().Set("Location", "/v1/groups/"+groupID)
		w.WriteHeader(http.StatusNoContent)
		logNoContent(ctx, log, "joined", "group_id", groupID, "outcome", string(outcome))
	default:
		logJSONError(ctx, log, "join_by_code_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID)
	}
}
