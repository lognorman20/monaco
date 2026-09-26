package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// NudgeInterval is how often anyone may remind a proposal's voters.
const NudgeInterval = time.Hour

// ClosingReminderLead is how long before a vote closes the "closes in 1 hour" reminder goes out.
const ClosingReminderLead = time.Hour

// ClosingReminderMinWindow keeps the reminder off short votes: a vote that is open for less than
// this was announced recently enough that a second buzz is noise.
const ClosingReminderMinWindow = 2 * time.Hour

// ErrNudgeNotAllowed means the caller neither proposed nor voted, so they cannot hurry others.
var ErrNudgeNotAllowed = errors.New("only the proposer or a member who voted can send a reminder")

// NudgeResult is what POST /v1/proposals/{id}/nudge reports.
type NudgeResult struct {
	// Reminded is how many members got the reminder (members who switched proposal
	// notifications off are not counted).
	Reminded int
	// WaitingOn is how many voters, the caller aside, have not voted.
	WaitingOn int
}

// SetNotifier wires inbox and push for governance events.
func (g *GovernanceService) SetNotifier(n *Notifier) {
	g.notifier = n
}

// NudgeProposal lets the proposer, or any member who already voted, remind the voters who have
// not. Once per proposal per NudgeInterval, whoever sends it.
func (g *GovernanceService) NudgeProposal(ctx context.Context, accessToken, proposalID string) (NudgeResult, error) {
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return NudgeResult{}, privy.ErrInvalidToken
		}
		return NudgeResult{}, fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return NudgeResult{}, err
	}
	if !found {
		return NudgeResult{}, ErrUserNotFound
	}
	if !isUUID(proposalID) {
		return NudgeResult{}, ErrProposalNotFound
	}
	row, found, err := g.store.GetProposalByID(ctx, proposalID)
	if err != nil {
		return NudgeResult{}, err
	}
	if !found {
		return NudgeResult{}, ErrProposalNotFound
	}
	member, err := g.store.IsGroupMember(ctx, row.GroupID, user.ID)
	if err != nil {
		return NudgeResult{}, err
	}
	if !member {
		// Same answer as an unknown id: a non-member learns nothing about the cabal's votes.
		return NudgeResult{}, ErrProposalNotFound
	}
	if readOnly, err := g.isFakerReadOnlyProposal(ctx, row); err != nil {
		return NudgeResult{}, err
	} else if readOnly {
		return NudgeResult{}, ErrFakerGroupReadOnly
	}
	now := g.now().UTC()
	if row.Status == domain.ProposalOpen && !now.Before(row.ExpiresAt) {
		if _, err := g.FinalizeExpiredProposal(ctx, proposalID); err != nil {
			return NudgeResult{}, err
		}
		return NudgeResult{}, ErrProposalNotOpen
	}
	if row.Status != domain.ProposalOpen {
		return NudgeResult{}, ErrProposalNotOpen
	}

	pending, voted, err := g.pendingVoters(ctx, row)
	if err != nil {
		return NudgeResult{}, err
	}
	if row.ProposerID != user.ID && !voted[user.ID] {
		return NudgeResult{}, ErrNudgeNotAllowed
	}
	pending = withoutID(pending, user.ID)
	result := NudgeResult{WaitingOn: len(pending)}
	if len(pending) == 0 {
		// Nothing to send, and nothing recorded: the hour is not spent on an empty reminder.
		return result, nil
	}

	if err := g.claimNudge(ctx, proposalID, user.ID, now); err != nil {
		return NudgeResult{}, err
	}
	title, body := proposalNudgeCopy(
		g.notifier.personName(ctx, user.ID), g.notifier.cabalName(ctx, row.GroupID),
		proposalRowSubject(row), untilClose(row.ExpiresAt, now),
	)
	result.Reminded = g.notifier.Notify(ctx, pending, Notification{
		Kind: NotifyProposalNudge, Title: title, Body: body,
		GroupID: row.GroupID, ProposalID: row.ID, Symbol: row.Symbol,
	})
	slog.InfoContext(ctx, "proposal nudge sent", "proposal_id", proposalID, "user_id", user.ID,
		"waiting_on", result.WaitingOn, "reminded", result.Reminded)
	return result, nil
}

// claimNudge records the reminder under a lock on the proposal row, or returns a
// *RateLimitError saying when the next one is allowed.
func (g *GovernanceService) claimNudge(ctx context.Context, proposalID, userID string, now time.Time) error {
	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, found, err := g.store.GetProposalForUpdateTx(ctx, tx, proposalID); err != nil {
		return err
	} else if !found {
		return ErrProposalNotFound
	}
	last, sent, err := g.store.LatestNudgeAtTx(ctx, tx, proposalID)
	if err != nil {
		return err
	}
	if sent {
		if next := last.Add(NudgeInterval); now.Before(next) {
			return &RateLimitError{RetryAfter: next.Sub(now)}
		}
	}
	if err := g.store.InsertNudgeTx(ctx, tx, proposalID, userID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit nudge: %w", err)
	}
	committed = true
	return nil
}

// RemindClosingProposal sends "closes in 1 hour" to the voters who have not voted, once per
// proposal (the reminder row is claimed first). Returns how many members it reached.
func (g *GovernanceService) RemindClosingProposal(ctx context.Context, row postgres.ProposalRow) (int, error) {
	now := g.now().UTC()
	claimed, err := g.store.ClaimProposalReminder(ctx, row.ID, now)
	if err != nil || !claimed {
		return 0, err
	}
	pending, _, err := g.pendingVoters(ctx, row)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}
	title, body := proposalExpiringCopy(g.notifier.cabalName(ctx, row.GroupID), proposalRowSubject(row), untilClose(row.ExpiresAt, now))
	return g.notifier.Notify(ctx, pending, Notification{
		Kind: NotifyProposalExpiring, Title: title, Body: body,
		GroupID: row.GroupID, ProposalID: row.ID, Symbol: row.Symbol,
	}), nil
}

// pendingVoters is the proposal's voter set minus everyone who has voted, and who has.
func (g *GovernanceService) pendingVoters(ctx context.Context, row postgres.ProposalRow) ([]string, map[string]bool, error) {
	rules, found, err := g.store.GetGroupRules(ctx, row.GroupID)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, ErrGroupNotFound
	}
	_, voterIDs, err := g.resolveVoterSet(ctx, row.GroupID, rules)
	if err != nil {
		return nil, nil, err
	}
	votes, err := g.store.ListVotesForProposal(ctx, row.ID)
	if err != nil {
		return nil, nil, err
	}
	voted := make(map[string]bool, len(votes))
	for _, v := range votes {
		voted[v.VoterID] = true
	}
	pending := make([]string, 0, len(voterIDs))
	for _, id := range voterIDs {
		if !voted[id] {
			pending = append(pending, id)
		}
	}
	return pending, voted, nil
}
