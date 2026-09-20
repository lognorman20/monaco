package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
	"github.com/monaco/monaco/packages/domain"
)

// ErrCommentParentNotFound means the reply target is missing or belongs to another proposal.
var ErrCommentParentNotFound = errors.New("parent comment not found")

// ErrCommentRateLimited means the author posted too many comments in the throttle window.
var ErrCommentRateLimited = errors.New("too many comments")

const (
	// commentThrottleLimit comments per commentThrottleWindow per author, across all proposals.
	commentThrottleLimit  = 10
	commentThrottleWindow = time.Minute
	// maxCommentsPerThread caps GET /v1/proposals/{id}/comments.
	maxCommentsPerThread = 500
)

// ProposalComment is one comment in a proposal thread. ParentID is empty for top-level comments.
type ProposalComment struct {
	ID         string
	ProposalID string
	ParentID   string
	AuthorID   string
	AuthorName string
	Body       string
	CreatedAt  time.Time
}

// CreateProposalCommentInput is POST /v1/proposals/{id}/comments.
type CreateProposalCommentInput struct {
	ProposalID      string
	ParentCommentID string
	Body            string
}

// ListProposalComments returns the flat, oldest-first thread for a proposal the viewer's group owns.
func (g *GovernanceService) ListProposalComments(ctx context.Context, accessToken, proposalID string) ([]ProposalComment, error) {
	_, proposal, err := g.authorizeProposalMember(ctx, accessToken, proposalID)
	if err != nil {
		return nil, err
	}

	rows, err := g.store.ListCommentsByProposal(ctx, proposal.ID, maxCommentsPerThread)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []ProposalComment{}, nil
	}

	authorIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		authorIDs = append(authorIDs, row.AuthorID)
	}
	names, err := g.store.ListUserDisplayNamesByIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}

	out := make([]ProposalComment, 0, len(rows))
	for _, row := range rows {
		out = append(out, proposalCommentFromRow(row, names[row.AuthorID]))
	}
	return out, nil
}

// CreateProposalComment posts a comment or reply as the authenticated group member.
func (g *GovernanceService) CreateProposalComment(ctx context.Context, accessToken string, in CreateProposalCommentInput) (ProposalComment, error) {
	body, err := domain.NormalizeProposalCommentBody(in.Body)
	if err != nil {
		return ProposalComment{}, err
	}
	parentID := strings.TrimSpace(in.ParentCommentID)
	if parentID != "" && !isUUID(parentID) {
		return ProposalComment{}, ErrCommentParentNotFound
	}

	userID, proposal, err := g.authorizeProposalMember(ctx, accessToken, in.ProposalID)
	if err != nil {
		return ProposalComment{}, err
	}

	recent, err := g.store.CountProposalCommentsByAuthorWithin(ctx, userID, commentThrottleWindow)
	if err != nil {
		return ProposalComment{}, err
	}
	if recent >= commentThrottleLimit {
		return ProposalComment{}, ErrCommentRateLimited
	}

	row, err := g.store.InsertProposalComment(ctx, proposal.ID, userID, parentID, body)
	if err != nil {
		if errors.Is(err, postgres.ErrCommentParentNotFound) {
			return ProposalComment{}, ErrCommentParentNotFound
		}
		return ProposalComment{}, err
	}

	names, err := g.store.ListUserDisplayNamesByIDs(ctx, []string{userID})
	if err != nil {
		return ProposalComment{}, err
	}
	return proposalCommentFromRow(row, names[userID]), nil
}

// authorizeProposalMember resolves the viewer and proposal, requiring group membership.
// Non-members get ErrGroupNotFound so callers can answer 404 without revealing the proposal exists.
func (g *GovernanceService) authorizeProposalMember(ctx context.Context, accessToken, proposalID string) (string, postgres.ProposalRow, error) {
	identity, err := g.privy.VerifySession(ctx, auth.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			return "", postgres.ProposalRow{}, auth.ErrUnauthorized
		}
		return "", postgres.ProposalRow{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := g.store.GetUserByDynamicUserID(ctx, identity.DynamicUserID)
	if err != nil {
		return "", postgres.ProposalRow{}, err
	}
	if !found {
		return "", postgres.ProposalRow{}, ErrUserNotFound
	}

	proposalID = strings.TrimSpace(proposalID)
	if !isUUID(proposalID) {
		return "", postgres.ProposalRow{}, ErrProposalNotFound
	}
	row, found, err := g.store.GetProposalByID(ctx, proposalID)
	if err != nil {
		return "", postgres.ProposalRow{}, err
	}
	if !found {
		return "", postgres.ProposalRow{}, ErrProposalNotFound
	}

	member, err := g.store.IsGroupMember(ctx, row.GroupID, user.ID)
	if err != nil {
		return "", postgres.ProposalRow{}, err
	}
	if !member {
		return "", postgres.ProposalRow{}, ErrGroupNotFound
	}
	return user.ID, row, nil
}

func proposalCommentFromRow(row postgres.ProposalCommentRow, authorName string) ProposalComment {
	if authorName == "" {
		authorName = "Member"
	}
	return ProposalComment{
		ID:         row.ID,
		ProposalID: row.ProposalID,
		ParentID:   row.ParentCommentID,
		AuthorID:   row.AuthorID,
		AuthorName: authorName,
		Body:       row.Body,
		CreatedAt:  row.CreatedAt,
	}
}
