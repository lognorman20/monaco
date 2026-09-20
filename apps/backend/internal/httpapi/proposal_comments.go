package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// maxCommentRequestBytes bounds the JSON body; the comment itself is capped at
// domain.MaxProposalCommentRunes code points (up to 4 bytes each) plus JSON escaping.
const maxCommentRequestBytes = 16 << 10

type createProposalCommentRequest struct {
	Body     string `json:"body"`
	ParentID string `json:"parentId"`
}

type proposalCommentResponse struct {
	ID         string `json:"id"`
	ProposalID string `json:"proposalId"`
	ParentID   string `json:"parentId,omitempty"`
	AuthorID   string `json:"authorId"`
	AuthorName string `json:"authorName"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
}

type listProposalCommentsResponse struct {
	Comments []proposalCommentResponse `json:"comments"`
}

// ListProposalCommentsHandler handles GET /v1/proposals/{id}/comments.
// Returns a flat list ordered oldest first; clients build the tree from parentId.
func (h *ProposalHandlers) ListProposalCommentsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/proposals/{id}/comments")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	proposalID := strings.TrimSpace(r.PathValue("id"))
	if proposalID == "" {
		logJSONError(ctx, log, "missing_proposal_id", w, http.StatusNotFound, "proposal not found")
		return
	}

	comments, err := h.Governance.ListProposalComments(ctx, token, proposalID)
	if err != nil {
		writeProposalCommentError(ctx, log, w, err, "proposal_id", proposalID)
		return
	}

	out := make([]proposalCommentResponse, 0, len(comments))
	for _, comment := range comments {
		out = append(out, proposalCommentToResponse(comment))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(listProposalCommentsResponse{Comments: out})
	logJSONOK(ctx, log, "ok", "proposal_id", proposalID, "count", len(out))
}

// CreateProposalCommentHandler handles POST /v1/proposals/{id}/comments.
func (h *ProposalHandlers) CreateProposalCommentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/proposals/{id}/comments")

	token, ok := bearerToken(r)
	if !ok {
		logJSONError(ctx, log, "missing_auth", w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}

	proposalID := strings.TrimSpace(r.PathValue("id"))
	if proposalID == "" {
		logJSONError(ctx, log, "missing_proposal_id", w, http.StatusNotFound, "proposal not found")
		return
	}

	var req createProposalCommentRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxCommentRequestBytes)
	if !decodeJSONBody(ctx, log, w, r, &req, "proposal_id", proposalID) {
		return
	}

	comment, err := h.Governance.CreateProposalComment(ctx, token, app.CreateProposalCommentInput{
		ProposalID:      proposalID,
		ParentCommentID: req.ParentID,
		Body:            req.Body,
	})
	if err != nil {
		writeProposalCommentError(ctx, log, w, err, "proposal_id", proposalID, "has_parent", req.ParentID != "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(proposalCommentToResponse(comment))
	log.done(ctx, "ok", http.StatusCreated, "proposal_id", proposalID, "comment_id", comment.ID, "author_id", comment.AuthorID, "has_parent", comment.ParentID != "")
}

func proposalCommentToResponse(comment app.ProposalComment) proposalCommentResponse {
	return proposalCommentResponse{
		ID:         comment.ID,
		ProposalID: comment.ProposalID,
		ParentID:   comment.ParentID,
		AuthorID:   comment.AuthorID,
		AuthorName: comment.AuthorName,
		Body:       comment.Body,
		CreatedAt:  comment.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// writeProposalCommentError maps comment errors. Non-members get 404 like GET /v1/proposals/{id}.
func writeProposalCommentError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound), errors.Is(err, app.ErrProposalNotFound), errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "proposal_not_found", w, http.StatusNotFound, "proposal not found", attrs...)
	case errors.Is(err, domain.ErrCommentBodyEmpty):
		logJSONError(ctx, log, "comment_empty", w, http.StatusBadRequest, "comment is required", attrs...)
	case errors.Is(err, domain.ErrCommentBodyTooLong):
		logJSONError(ctx, log, "comment_too_long", w, http.StatusBadRequest, "comment is too long", attrs...)
	case errors.Is(err, domain.ErrCommentBodyInvalid):
		logJSONError(ctx, log, "comment_invalid", w, http.StatusBadRequest, "comment contains invalid characters", attrs...)
	case errors.Is(err, app.ErrCommentParentNotFound):
		logJSONError(ctx, log, "parent_not_found", w, http.StatusBadRequest, "reply target not found on this proposal", attrs...)
	case errors.Is(err, app.ErrCommentRateLimited):
		w.Header().Set("Retry-After", "60")
		logJSONError(ctx, log, "comment_rate_limited", w, http.StatusTooManyRequests, "too many comments, try again in a minute", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "proposal_comment_failed", w, http.StatusInternalServerError, "internal server error", all...)
	}
}
