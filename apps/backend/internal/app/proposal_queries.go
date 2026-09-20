package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// ProposalListItem is one row in GET /v1/groups/{id}/proposals.
type ProposalListItem struct {
	ID          string
	Symbol      string
	Kind        domain.ProposalKind
	UsdcMicros  int64
	TokenAmount int64
	// AgentDisplayName and AllocationUsdcMicros are set on agent governance proposals.
	AgentDisplayName     string
	AllocationUsdcMicros int64
	Thesis               string
	Status               ProposalStatus
	ProposerID           string
	ProposerName         string
	CreatedAt            time.Time
	ExpiresAt            time.Time
	CanVote              bool
	VoteSummary          ProposalVoteSummary
	CommentCount         int
}

// ProposalVoteDetail is one ballot on a proposal detail view.
type ProposalVoteDetail struct {
	VoterID     string
	DisplayName string
	Choice      string
	CastAt      *time.Time
}

// ProposalVoteSummary is vote outcome metadata for proposal detail.
type ProposalVoteSummary struct {
	YesCount      int
	NoCount       int
	EligibleCount int
	Threshold     string
}

// ProposalExecutionDetail is on-chain swap status for a proposal.
type ProposalExecutionDetail struct {
	State            string
	TxSignature      string
	TransactionID    string
	ExecuteRequestID string
	ExecutedAt       *time.Time
	FailureReason    string
}

// ProposalDetailResult is GET /v1/proposals/{id}.
type ProposalDetailResult struct {
	ID                   string
	GroupID              string
	Symbol               string
	Kind                 domain.ProposalKind
	UsdcMicros           int64
	TokenAmount          int64
	AgentDisplayName     string
	AllocationUsdcMicros int64
	MintedAgentKey       string
	Thesis               string
	Status               ProposalStatus
	CreatedAt            time.Time
	ExpiresAt            time.Time
	ProposerID           string
	ProposerName         string
	CanVote              bool
	Votes                []ProposalVoteDetail
	VoteSummary          ProposalVoteSummary
	Execution            ProposalExecutionDetail
	CommentCount         int
}

// ListGroupProposals returns open or closed proposals for a group member.
func (g *GovernanceService) ListGroupProposals(ctx context.Context, accessToken, groupID, tab string) ([]ProposalListItem, error) {
	if strings.TrimSpace(groupID) == "" {
		return nil, fmt.Errorf("group id is required")
	}

	// Members read their club; any authed user may spectate a faker scale club (#153).
	userID, err := authorizeGroupReader(ctx, g.store, g.privy, accessToken, groupID)
	if err != nil {
		return nil, err
	}

	var statuses []domain.ProposalStatus
	switch strings.ToLower(strings.TrimSpace(tab)) {
	case "", "open":
		statuses = []domain.ProposalStatus{ProposalOpen}
	case "closed":
		statuses = []domain.ProposalStatus{ProposalPassed, ProposalFailed, ProposalExpired}
	default:
		return nil, fmt.Errorf("invalid tab")
	}

	rows, err := g.store.ListProposalsByGroupID(ctx, groupID, statuses)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []ProposalListItem{}, nil
	}

	proposerIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		proposerIDs = append(proposerIDs, row.ProposerID)
	}
	displayNames, err := g.store.ListUserDisplayNamesByIDs(ctx, proposerIDs)
	if err != nil {
		return nil, err
	}

	proposalIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		proposalIDs = append(proposalIDs, row.ID)
	}
	stats, err := g.store.ListProposalFeedStats(ctx, proposalIDs, userID)
	if err != nil {
		return nil, err
	}

	eligibility, err := g.groupVoteEligibility(ctx, groupID, userID)
	if err != nil {
		return nil, err
	}
	readOnlyRows, err := g.fakerReadOnlyProposalIDs(ctx, groupID, rows)
	if err != nil {
		return nil, err
	}
	readOnlyEligibility := eligibility
	if len(readOnlyRows) > 0 {
		if readOnlyEligibility, err = g.fakerReadOnlyEligibility(ctx, groupID, eligibility); err != nil {
			return nil, err
		}
	}

	nowUnix := g.now().UTC().Unix()
	items := make([]ProposalListItem, 0, len(rows))
	for _, row := range rows {
		name := displayNames[row.ProposerID]
		if name == "" {
			name = "Member"
		}
		rowStats := stats[row.ID]
		rowEligibility := eligibility
		if readOnlyRows[row.ID] {
			rowEligibility = readOnlyEligibility
		}
		items = append(items, ProposalListItem{
			ID:                   row.ID,
			Symbol:               row.Symbol,
			Kind:                 row.Kind,
			UsdcMicros:           row.UsdcMicros,
			TokenAmount:          row.TokenAmount,
			AgentDisplayName:     row.AgentDisplayName,
			AllocationUsdcMicros: row.AllocationUsdcMicros,
			Thesis:               row.Thesis,
			Status:               row.Status,
			ProposerID:           row.ProposerID,
			ProposerName:         name,
			CreatedAt:            row.CreatedAt,
			ExpiresAt:            row.ExpiresAt,
			CanVote: row.Status == ProposalOpen &&
				nowUnix < row.ExpiresAt.UTC().Unix() &&
				rowEligibility.viewerMayVote &&
				!rowStats.ViewerVoted,
			VoteSummary: ProposalVoteSummary{
				YesCount:      rowStats.YesCount,
				NoCount:       rowStats.NoCount,
				EligibleCount: rowEligibility.eligibleCount,
				Threshold:     rowEligibility.threshold,
			},
			CommentCount: rowStats.CommentCount,
		})
	}
	return items, nil
}

// GetProposalDetail returns proposal metadata, votes, and whether the viewer may vote.
func (g *GovernanceService) GetProposalDetail(ctx context.Context, accessToken, proposalID string) (ProposalDetailResult, error) {
	if strings.TrimSpace(proposalID) == "" {
		return ProposalDetailResult{}, fmt.Errorf("proposal id is required")
	}

	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return ProposalDetailResult{}, privy.ErrInvalidToken
		}
		return ProposalDetailResult{}, fmt.Errorf("verify session: %w", err)
	}

	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return ProposalDetailResult{}, err
	}
	if !found {
		return ProposalDetailResult{}, ErrUserNotFound
	}

	if !isUUID(proposalID) {
		return ProposalDetailResult{}, ErrProposalNotFound
	}
	row, found, err := g.store.GetProposalByID(ctx, proposalID)
	if err != nil {
		return ProposalDetailResult{}, err
	}
	if !found {
		return ProposalDetailResult{}, ErrProposalNotFound
	}

	readable, err := g.store.CanReadGroup(ctx, row.GroupID, user.ID)
	if err != nil {
		return ProposalDetailResult{}, err
	}
	if !readable {
		return ProposalDetailResult{}, ErrGroupNotFound
	}

	// Faker scale clubs and faker-proposed proposals (#153) are display-only: no voting,
	// eligibility shows every member (ghost ballots included), and no execution is pending.
	fakerReadOnly, err := g.isFakerReadOnlyProposal(ctx, row)
	if err != nil {
		return ProposalDetailResult{}, err
	}

	proposal := proposalFromRow(row)
	if proposal.Status == ProposalOpen && g.now().UTC().Unix() >= proposal.ExpiresAt {
		updated, err := g.FinalizeExpiredProposal(ctx, proposalID)
		if err != nil {
			return ProposalDetailResult{}, err
		}
		proposal = updated
		row, found, err = g.store.GetProposalByID(ctx, proposalID)
		if err != nil {
			return ProposalDetailResult{}, err
		}
		if !found {
			return ProposalDetailResult{}, ErrProposalNotFound
		}
	}

	names, err := g.store.ListUserDisplayNamesByIDs(ctx, []string{row.ProposerID})
	if err != nil {
		return ProposalDetailResult{}, err
	}
	proposerName := names[row.ProposerID]
	if proposerName == "" {
		proposerName = "Member"
	}

	voteRows, err := g.store.ListVotesForProposal(ctx, proposalID)
	if err != nil {
		return ProposalDetailResult{}, err
	}

	voterIDs := make([]string, 0, len(voteRows))
	for _, vote := range voteRows {
		voterIDs = append(voterIDs, vote.VoterID)
	}
	voterNames, err := g.store.ListUserDisplayNamesByIDs(ctx, voterIDs)
	if err != nil {
		return ProposalDetailResult{}, err
	}

	votes := make([]ProposalVoteDetail, 0, len(voteRows))
	for _, vote := range voteRows {
		name := voterNames[vote.VoterID]
		if name == "" {
			name = "Member"
		}
		castAt := vote.CastAt
		votes = append(votes, ProposalVoteDetail{
			VoterID:     vote.VoterID,
			DisplayName: name,
			Choice:      string(vote.Choice),
			CastAt:      &castAt,
		})
	}

	eligibility, err := g.groupVoteEligibility(ctx, row.GroupID, user.ID)
	if err != nil {
		return ProposalDetailResult{}, err
	}
	if fakerReadOnly {
		if eligibility, err = g.fakerReadOnlyEligibility(ctx, row.GroupID, eligibility); err != nil {
			return ProposalDetailResult{}, err
		}
	}
	stats, err := g.store.ListProposalFeedStats(ctx, []string{row.ID}, user.ID)
	if err != nil {
		return ProposalDetailResult{}, err
	}
	canVote := proposal.Status == ProposalOpen &&
		g.now().UTC().Unix() < proposal.ExpiresAt &&
		eligibility.viewerMayVote &&
		!stats[row.ID].ViewerVoted

	voteSummary := ProposalVoteSummary{
		Threshold:     eligibility.threshold,
		EligibleCount: eligibility.eligibleCount,
	}
	for _, vote := range votes {
		switch vote.Choice {
		case string(domain.VoteYes):
			voteSummary.YesCount++
		case string(domain.VoteNo):
			voteSummary.NoCount++
		}
	}

	var execution ProposalExecutionDetail
	if domain.IsAgentGovernanceKind(row.Kind) {
		execution = ProposalExecutionDetail{State: "not_applicable"}
	} else {
		action := postgres.TransactionActionBuy
		if row.Kind == domain.ProposalKindSell {
			action = postgres.TransactionActionSell
		}
		txRow, txFound, err := g.store.GetLatestTransactionByProposalAndAction(ctx, proposalID, action)
		if err != nil {
			return ProposalDetailResult{}, err
		}
		execution = buildProposalExecutionDetail(proposal.Status, txRow, txFound)
		if fakerReadOnly && !txFound {
			// Seeded passed proposals without a swap never execute (the poller skips them).
			execution = ProposalExecutionDetail{State: "not_applicable"}
		}
	}

	mintedKey := ""
	if row.Kind == domain.ProposalKindAddAgent {
		if key, ok, err := g.RevealAgentKeyForProposer(ctx, proposalID, row.ProposerID, user.ID, proposal.Status); err != nil {
			return ProposalDetailResult{}, err
		} else if ok {
			mintedKey = key
		}
	}

	return ProposalDetailResult{
		ID:                   row.ID,
		GroupID:              row.GroupID,
		Symbol:               row.Symbol,
		Kind:                 row.Kind,
		UsdcMicros:           row.UsdcMicros,
		TokenAmount:          row.TokenAmount,
		AgentDisplayName:     row.AgentDisplayName,
		AllocationUsdcMicros: row.AllocationUsdcMicros,
		MintedAgentKey:       mintedKey,
		Thesis:               row.Thesis,
		Status:               row.Status,
		CreatedAt:            row.CreatedAt,
		ExpiresAt:            row.ExpiresAt,
		ProposerID:           row.ProposerID,
		ProposerName:         proposerName,
		CanVote:              canVote,
		Votes:                votes,
		VoteSummary:          voteSummary,
		Execution:            execution,
		CommentCount:         stats[row.ID].CommentCount,
	}, nil
}

type groupVoteEligibility struct {
	threshold     string
	eligibleCount int
	viewerMayVote bool
}

// groupVoteEligibility resolves the group's voter set once per feed page.
func (g *GovernanceService) groupVoteEligibility(ctx context.Context, groupID, viewerID string) (groupVoteEligibility, error) {
	out := groupVoteEligibility{threshold: string(ThresholdMajority)}
	rules, found, err := g.store.GetGroupRules(ctx, groupID)
	if err != nil {
		return groupVoteEligibility{}, err
	}
	if !found {
		return out, nil
	}
	out.threshold = string(rules.Threshold)
	voterSet, eligibleIDs, err := g.resolveVoterSet(ctx, groupID, rules)
	if err != nil {
		return groupVoteEligibility{}, err
	}
	out.eligibleCount = len(eligibleIDs)
	out.viewerMayVote = domain.MemberMayVote(voterSet, viewerID, eligibleIDs)
	return out, nil
}

func buildProposalExecutionDetail(status ProposalStatus, tx postgres.TransactionRow, found bool) ProposalExecutionDetail {
	if status != ProposalPassed {
		return ProposalExecutionDetail{State: "not_applicable"}
	}
	if !found {
		return ProposalExecutionDetail{State: "pending"}
	}

	detail := ProposalExecutionDetail{
		TransactionID: tx.ID,
	}
	if tx.ExecuteRequestID.Valid {
		detail.ExecuteRequestID = tx.ExecuteRequestID.String
	}
	if tx.TxSignature.Valid {
		detail.TxSignature = tx.TxSignature.String
	}
	if tx.ConfirmedAt.Valid {
		at := tx.ConfirmedAt.Time.UTC()
		detail.ExecutedAt = &at
	}

	switch tx.Status {
	case postgres.TransactionStatusConfirmed:
		detail.State = "confirmed"
	case postgres.TransactionStatusFailed:
		detail.State = "failed"
		detail.FailureReason = "swap failed"
	default:
		detail.State = "pending"
	}
	return detail
}

// isFakerReadOnlyProposal reports whether a proposal belongs to a faker scale club or was
// proposed by a faker user (ghost proposal in a real group). Such proposals are display-only.
func (g *GovernanceService) isFakerReadOnlyProposal(ctx context.Context, row postgres.ProposalRow) (bool, error) {
	fakerGroup, err := g.store.IsFakerGroup(ctx, row.GroupID)
	if err != nil {
		return false, err
	}
	if fakerGroup {
		return true, nil
	}
	return g.store.IsFakerUser(ctx, row.ProposerID)
}

// fakerReadOnlyEligibility makes a faker proposal display-only (#153): nobody may vote, and
// eligibility counts every member so seeded ghost ballots read as a real tally.
func (g *GovernanceService) fakerReadOnlyEligibility(ctx context.Context, groupID string, base groupVoteEligibility) (groupVoteEligibility, error) {
	memberIDs, err := g.store.ListGroupMemberIDs(ctx, groupID)
	if err != nil {
		return groupVoteEligibility{}, err
	}
	base.eligibleCount = len(memberIDs)
	base.viewerMayVote = false
	return base, nil
}

// fakerReadOnlyProposalIDs returns the feed rows that are display-only: every row in a faker
// scale club, or rows a faker user proposed in a real club. One lookup per distinct proposer.
func (g *GovernanceService) fakerReadOnlyProposalIDs(ctx context.Context, groupID string, rows []postgres.ProposalRow) (map[string]bool, error) {
	out := make(map[string]bool)
	fakerGroup, err := g.store.IsFakerGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	fakerProposer := make(map[string]bool)
	for _, row := range rows {
		if fakerGroup {
			out[row.ID] = true
			continue
		}
		isFaker, seen := fakerProposer[row.ProposerID]
		if !seen {
			if isFaker, err = g.store.IsFakerUser(ctx, row.ProposerID); err != nil {
				return nil, err
			}
			fakerProposer[row.ProposerID] = isFaker
		}
		if isFaker {
			out[row.ID] = true
		}
	}
	return out, nil
}
