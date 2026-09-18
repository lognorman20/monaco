package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// ProposalHandlers serves proposal create and vote HTTP routes.
type ProposalHandlers struct {
	Store      *postgres.Store
	Privy      privy.Client
	Governance *app.GovernanceService
}

type createProposalRequest struct {
	Symbol string `json:"symbol"`
	USDC   int64  `json:"usdc"`
}

type createProposalResponse struct {
	ProposalID string `json:"proposalId"`
}

type castVoteRequest struct {
	Choice string `json:"choice"`
}

// CreateProposalHandler handles POST /v1/groups/{id}/proposals.
func (h *ProposalHandlers) CreateProposalHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/groups/{id}/proposals")

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

	var req createProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", "group_id", groupID)
		return
	}
	if strings.TrimSpace(req.Symbol) == "" {
		logJSONError(ctx, log, "missing_symbol", w, http.StatusBadRequest, "symbol is required", "group_id", groupID)
		return
	}
	if req.USDC <= 0 {
		logJSONError(ctx, log, "invalid_usdc", w, http.StatusBadRequest, "usdc must be positive", "group_id", groupID, "symbol", req.Symbol)
		return
	}

	userID, err := h.authorizeUser(ctx, token)
	if err != nil {
		writeProposalError(ctx, log, w, err, "group_id", groupID, "symbol", req.Symbol)
		return
	}

	proposal, err := h.Governance.CreateProposal(ctx, app.CreateProposalInput{
		GroupID:    groupID,
		ProposerID: userID,
		Symbol:     strings.TrimSpace(req.Symbol),
		UsdcMicros: req.USDC,
	})
	if err != nil {
		writeProposalCreateError(ctx, log, w, err, "group_id", groupID, "user_id", userID, "symbol", req.Symbol)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(createProposalResponse{ProposalID: proposal.ID})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "user_id", userID, "proposal_id", proposal.ID, "symbol", req.Symbol)
}

// CastVoteHandler handles POST /v1/proposals/{id}/votes.
func (h *ProposalHandlers) CastVoteHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "POST /v1/proposals/{id}/votes")

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

	var req castVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logJSONError(ctx, log, "invalid_body", w, http.StatusBadRequest, "invalid request body", "proposal_id", proposalID)
		return
	}

	choice, err := domain.ParseVoteChoice(strings.TrimSpace(req.Choice))
	if err != nil {
		logJSONError(ctx, log, "invalid_choice", w, http.StatusBadRequest, "invalid vote choice", "proposal_id", proposalID)
		return
	}

	userID, err := h.authorizeUser(ctx, token)
	if err != nil {
		writeProposalError(ctx, log, w, err, "proposal_id", proposalID)
		return
	}

	_, err = h.Governance.CastVote(ctx, app.CastVoteInput{
		ProposalID: proposalID,
		VoterID:    userID,
		Choice:     choice,
	})
	if err != nil {
		writeProposalVoteError(ctx, log, w, err, "proposal_id", proposalID, "user_id", userID)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	logJSONOK(ctx, log, "ok", "proposal_id", proposalID, "user_id", userID, "choice", choice)
}

type proposalListItemResponse struct {
	ID           string `json:"id"`
	Symbol       string `json:"symbol"`
	UsdcMicros   string `json:"usdcMicros"`
	Status       string `json:"status"`
	ProposerID   string `json:"proposerId"`
	ProposerName string `json:"proposerName"`
	CreatedAt    string `json:"createdAt"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

type listGroupProposalsResponse struct {
	Proposals []proposalListItemResponse `json:"proposals"`
}

type proposalVoteResponse struct {
	VoterID     string `json:"voterId"`
	DisplayName string `json:"displayName"`
	Choice      string `json:"choice"`
	CastAt      string `json:"castAt,omitempty"`
}

type proposalVoteSummaryResponse struct {
	YesCount      int    `json:"yesCount"`
	NoCount       int    `json:"noCount"`
	EligibleCount int    `json:"eligibleCount"`
	Threshold     string `json:"threshold"`
}

type proposalExecutionResponse struct {
	State            string `json:"state"`
	TxSignature      string `json:"txSignature,omitempty"`
	TransactionID    string `json:"transactionId,omitempty"`
	ExecuteRequestID string `json:"executeRequestId,omitempty"`
	ExecutedAt       string `json:"executedAt,omitempty"`
	FailureReason    string `json:"failureReason,omitempty"`
}

type proposalDetailResponse struct {
	ID           string                      `json:"id"`
	GroupID      string                      `json:"groupId"`
	Symbol       string                      `json:"symbol"`
	UsdcMicros   string                      `json:"usdcMicros"`
	Status       string                      `json:"status"`
	CreatedAt    string                      `json:"createdAt"`
	ExpiresAt    string                      `json:"expiresAt"`
	ProposerID   string                      `json:"proposerId"`
	ProposerName string                      `json:"proposerName"`
	CanVote      bool                        `json:"canVote"`
	Votes        []proposalVoteResponse      `json:"votes"`
	VoteSummary  proposalVoteSummaryResponse `json:"voteSummary"`
	Execution    proposalExecutionResponse   `json:"execution"`
}

// ListGroupProposalsHandler handles GET /v1/groups/{id}/proposals.
func (h *ProposalHandlers) ListGroupProposalsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/groups/{id}/proposals")

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

	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	items, err := h.Governance.ListGroupProposals(ctx, token, groupID, tab)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "group_id", groupID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) {
			logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", "group_id", groupID)
			return
		}
		if strings.Contains(err.Error(), "invalid tab") {
			logJSONError(ctx, log, "invalid_tab", w, http.StatusBadRequest, "invalid tab", "group_id", groupID)
			return
		}
		logJSONError(ctx, log, "list_proposals_failed", w, http.StatusInternalServerError, "internal server error", "group_id", groupID, "err", err.Error())
		return
	}

	proposals := make([]proposalListItemResponse, 0, len(items))
	for _, item := range items {
		row := proposalListItemResponse{
			ID:           item.ID,
			Symbol:       item.Symbol,
			UsdcMicros:   strconv.FormatInt(item.UsdcMicros, 10),
			Status:       string(item.Status),
			ProposerID:   item.ProposerID,
			ProposerName: item.ProposerName,
			CreatedAt:    item.CreatedAt.UTC().Format(time.RFC3339),
		}
		if item.Status == app.ProposalOpen {
			row.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
		}
		proposals = append(proposals, row)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(listGroupProposalsResponse{Proposals: proposals})
	logJSONOK(ctx, log, "ok", "group_id", groupID, "tab", tab, "count", len(proposals))
}

// GetProposalDetailHandler handles GET /v1/proposals/{id}.
func (h *ProposalHandlers) GetProposalDetailHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /v1/proposals/{id}")

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

	detail, err := h.Governance.GetProposalDetail(ctx, token, proposalID)
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", "proposal_id", proposalID)
			return
		}
		if errors.Is(err, app.ErrUserNotFound) || errors.Is(err, app.ErrGroupNotFound) || errors.Is(err, app.ErrProposalNotFound) {
			logJSONError(ctx, log, "proposal_not_found", w, http.StatusNotFound, "proposal not found", "proposal_id", proposalID)
			return
		}
		logJSONError(ctx, log, "get_proposal_failed", w, http.StatusInternalServerError, "internal server error", "proposal_id", proposalID, "err", err.Error())
		return
	}

	votes := make([]proposalVoteResponse, 0, len(detail.Votes))
	for _, vote := range detail.Votes {
		row := proposalVoteResponse{
			VoterID:     vote.VoterID,
			DisplayName: vote.DisplayName,
			Choice:      vote.Choice,
		}
		if vote.CastAt != nil {
			row.CastAt = vote.CastAt.UTC().Format(time.RFC3339)
		}
		votes = append(votes, row)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	execution := proposalExecutionResponse{State: detail.Execution.State}
	if detail.Execution.TxSignature != "" {
		execution.TxSignature = detail.Execution.TxSignature
	}
	if detail.Execution.TransactionID != "" {
		execution.TransactionID = detail.Execution.TransactionID
	}
	if detail.Execution.ExecuteRequestID != "" {
		execution.ExecuteRequestID = detail.Execution.ExecuteRequestID
	}
	if detail.Execution.ExecutedAt != nil {
		execution.ExecutedAt = detail.Execution.ExecutedAt.UTC().Format(time.RFC3339)
	}
	if detail.Execution.FailureReason != "" {
		execution.FailureReason = detail.Execution.FailureReason
	}
	_ = json.NewEncoder(w).Encode(proposalDetailResponse{
		ID:           detail.ID,
		GroupID:      detail.GroupID,
		Symbol:       detail.Symbol,
		UsdcMicros:   strconv.FormatInt(detail.UsdcMicros, 10),
		Status:       string(detail.Status),
		CreatedAt:    detail.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:    detail.ExpiresAt.UTC().Format(time.RFC3339),
		ProposerID:   detail.ProposerID,
		ProposerName: detail.ProposerName,
		CanVote:      detail.CanVote,
		Votes:        votes,
		VoteSummary: proposalVoteSummaryResponse{
			YesCount:      detail.VoteSummary.YesCount,
			NoCount:       detail.VoteSummary.NoCount,
			EligibleCount: detail.VoteSummary.EligibleCount,
			Threshold:     detail.VoteSummary.Threshold,
		},
		Execution: execution,
	})
	logJSONOK(ctx, log, "ok", "proposal_id", proposalID, "status", detail.Status)
}

func (h *ProposalHandlers) authorizeUser(ctx context.Context, accessToken string) (string, error) {
	identity, err := h.Privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}

	user, found, err := h.Store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", app.ErrUserNotFound
	}
	return user.ID, nil
}

func writeProposalError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "internal_error", w, http.StatusInternalServerError, "internal server error", all...)
	}
}

func writeProposalCreateError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrGroupNotFound):
		logJSONError(ctx, log, "group_not_found", w, http.StatusNotFound, "group not found", attrs...)
	case errors.Is(err, app.ErrNotGroupMember):
		logJSONError(ctx, log, "not_group_member", w, http.StatusForbidden, "not a group member", attrs...)
	case errors.Is(err, app.ErrNotEligibleProposer):
		logJSONError(ctx, log, "not_eligible_proposer", w, http.StatusForbidden, "not eligible to propose", attrs...)
	case errors.Is(err, app.ErrQuoteNotRoutable):
		logJSONError(ctx, log, "quote_not_routable", w, http.StatusBadRequest, "quote not routable", attrs...)
	case errors.Is(err, app.ErrExceedsTreasuryUSDC):
		logJSONError(ctx, log, "exceeds_treasury_usdc", w, http.StatusBadRequest, "amount exceeds treasury total available", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "create_proposal_failed", w, http.StatusInternalServerError, "internal server error", all...)
	}
}

func writeProposalVoteError(ctx context.Context, log *requestLog, w http.ResponseWriter, err error, attrs ...any) {
	switch {
	case errors.Is(err, privy.ErrInvalidToken):
		logJSONError(ctx, log, "invalid_token", w, http.StatusUnauthorized, "invalid or expired access token", attrs...)
	case errors.Is(err, app.ErrUserNotFound):
		logJSONError(ctx, log, "user_not_found", w, http.StatusNotFound, "user not found", attrs...)
	case errors.Is(err, app.ErrProposalNotFound):
		logJSONError(ctx, log, "proposal_not_found", w, http.StatusNotFound, "proposal not found", attrs...)
	case errors.Is(err, app.ErrNotEligibleVoter):
		logJSONError(ctx, log, "not_eligible_voter", w, http.StatusForbidden, "not eligible to vote", attrs...)
	case errors.Is(err, app.ErrProposalNotOpen):
		logJSONError(ctx, log, "proposal_not_open", w, http.StatusConflict, "proposal not open", attrs...)
	default:
		all := append(attrs, "err", err.Error())
		logJSONError(ctx, log, "cast_vote_failed", w, http.StatusInternalServerError, "internal server error", all...)
	}
}
