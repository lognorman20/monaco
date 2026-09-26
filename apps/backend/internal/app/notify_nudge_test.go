package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

func (c *notifyCabal) vote(t *testing.T, proposalID string, member int, choice domain.VoteChoice) {
	t.Helper()
	if _, err := c.h.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: proposalID, VoterID: c.members[member], Choice: choice}); err != nil {
		t.Fatalf("vote: %v", err)
	}
}

func TestNudgeProposal_remindsOnlyTheMembersWhoHaveNotVoted(t *testing.T) {
	// Arrange: five members; Jordan proposes, Priya votes yes, three have not voted.
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam", "Ada", "Lee")
	p := c.propose(t, 0, 250_000_000)
	c.vote(t, p.ID, 1, domain.VoteYes)

	// Act: Priya, who voted, sends the reminder.
	result, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[1], p.ID)

	// Assert
	if err != nil {
		t.Fatalf("NudgeProposal: %v", err)
	}
	// Jordan (the proposer, not voted), Sam, Ada and Lee.
	if result.WaitingOn != 4 || result.Reminded != 4 {
		t.Fatalf("result = %+v, want 4 waiting and 4 reminded", result)
	}
	if countKind(c.inbox(t, 1), NotifyProposalNudge) != 0 {
		t.Fatal("the sender reminded herself")
	}
	row := firstOfKind(t, c.inbox(t, 2), NotifyProposalNudge)
	if row.Title != "Priya is waiting on your vote" {
		t.Fatalf("title = %q", row.Title)
	}
	if row.Body != "$250 of Apple in "+c.cabalName(t)+". Voting closes in 1 day." {
		t.Fatalf("body = %q", row.Body)
	}
}

func TestNudgeProposal_oncePerProposalPerHour(t *testing.T) {
	cases := []struct {
		name      string
		after     time.Duration
		wantRetry time.Duration
	}{
		{name: "a minute later is refused", after: time.Minute, wantRetry: 59 * time.Minute},
		{name: "59 minutes later is refused", after: 59 * time.Minute, wantRetry: time.Minute},
		{name: "an hour later is allowed", after: time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			c := newNotifyCabal(t, "Jordan", "Priya", "Sam")
			p := c.propose(t, 0, 10_000_000)
			if _, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[0], p.ID); err != nil {
				t.Fatalf("first nudge: %v", err)
			}
			c.vote(t, p.ID, 1, domain.VoteYes)
			c.now = c.now.Add(tc.after)

			// Act: someone else tries; the limit is per proposal, not per sender.
			_, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[1], p.ID)

			// Assert
			if tc.wantRetry == 0 {
				if err != nil {
					t.Fatalf("second nudge: %v", err)
				}
				return
			}
			var limited *RateLimitError
			if !errors.As(err, &limited) || !errors.Is(err, ErrRateLimited) {
				t.Fatalf("err = %v, want a rate limit", err)
			}
			if limited.RetryAfter != tc.wantRetry {
				t.Fatalf("retry after = %s, want %s", limited.RetryAfter, tc.wantRetry)
			}
		})
	}
}

func TestNudgeProposal_refusals(t *testing.T) {
	t.Run("a member who has not voted and did not propose", func(t *testing.T) {
		c := newNotifyCabal(t, "Jordan", "Priya", "Sam")
		p := c.propose(t, 0, 10_000_000)
		if _, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[2], p.ID); !errors.Is(err, ErrNudgeNotAllowed) {
			t.Fatalf("err = %v, want ErrNudgeNotAllowed", err)
		}
	})
	t.Run("a closed proposal", func(t *testing.T) {
		c := newNotifyCabal(t, "Jordan", "Priya")
		p := c.propose(t, 0, 10_000_000)
		// Two members, majority: one no already decides it.
		c.vote(t, p.ID, 1, domain.VoteNo)
		if _, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[0], p.ID); !errors.Is(err, ErrProposalNotOpen) {
			t.Fatalf("err = %v, want ErrProposalNotOpen", err)
		}
	})
	t.Run("a proposal past its deadline", func(t *testing.T) {
		c := newNotifyCabal(t, "Jordan", "Priya")
		p := c.propose(t, 0, 10_000_000)
		c.now = c.now.Add(25 * time.Hour)
		if _, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[0], p.ID); !errors.Is(err, ErrProposalNotOpen) {
			t.Fatalf("err = %v, want ErrProposalNotOpen", err)
		}
	})
	t.Run("someone outside the cabal", func(t *testing.T) {
		c := newNotifyCabal(t, "Jordan", "Priya")
		p := c.propose(t, 0, 10_000_000)
		openTestSession(t, c.h.ISO, c.h.Sessions, c.h.Privy, "outsider", "Out")
		if _, err := c.h.Governance.NudgeProposal(context.Background(), c.h.ISO.UniqueToken("outsider"), p.ID); !errors.Is(err, ErrProposalNotFound) {
			t.Fatalf("err = %v, want ErrProposalNotFound", err)
		}
	})
	t.Run("an unknown id", func(t *testing.T) {
		c := newNotifyCabal(t, "Jordan")
		if _, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[0], "not-a-uuid"); !errors.Is(err, ErrProposalNotFound) {
			t.Fatalf("err = %v, want ErrProposalNotFound", err)
		}
	})
}

func TestNudgeProposal_nobodyLeftToRemindSendsNothingAndKeepsTheHour(t *testing.T) {
	// Arrange: Priya yes, Sam no; only Jordan, the proposer, has not voted.
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam")
	p := c.propose(t, 0, 10_000_000)
	c.vote(t, p.ID, 1, domain.VoteYes)
	c.vote(t, p.ID, 2, domain.VoteNo)

	// Act: Jordan has nobody to remind but himself.
	empty, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[0], p.ID)
	if err != nil {
		t.Fatalf("empty nudge: %v", err)
	}
	// Priya can still remind Jordan: the empty reminder did not spend the hour.
	sent, err := c.h.Governance.NudgeProposal(context.Background(), c.tokens[1], p.ID)

	// Assert
	if empty.WaitingOn != 0 || empty.Reminded != 0 {
		t.Fatalf("empty = %+v", empty)
	}
	if err != nil || sent.Reminded != 1 {
		t.Fatalf("sent = %+v, %v; want Jordan reminded", sent, err)
	}
}

func TestRemindClosingProposal_onceToTheVotersWhoHaveNotVoted(t *testing.T) {
	// Arrange
	c := newNotifyCabal(t, "Jordan", "Priya", "Sam")
	p := c.propose(t, 0, 10_000_000)
	c.vote(t, p.ID, 1, domain.VoteYes)
	row, _, err := c.h.Store.GetProposalByID(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	c.now = row.ExpiresAt.Add(-50 * time.Minute)

	// Act: twice, as two ticks (or two processes) would.
	first, err := c.h.Governance.RemindClosingProposal(context.Background(), row)
	if err != nil {
		t.Fatalf("remind: %v", err)
	}
	second, err := c.h.Governance.RemindClosingProposal(context.Background(), row)
	if err != nil {
		t.Fatalf("remind again: %v", err)
	}

	// Assert
	if first != 2 || second != 0 {
		t.Fatalf("reminded %d then %d, want 2 then 0", first, second)
	}
	if countKind(c.inbox(t, 1), NotifyProposalExpiring) != 0 {
		t.Fatal("a member who voted was reminded")
	}
	got := firstOfKind(t, c.inbox(t, 2), NotifyProposalExpiring)
	if got.Title != "Voting on $10 of Apple closes in 50 minutes" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Body != c.cabalName(t)+" is waiting on your vote." {
		t.Fatalf("body = %q", got.Body)
	}
}
