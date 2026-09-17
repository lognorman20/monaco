package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

type governanceHarness struct {
	Governance *GovernanceService
	Store      *postgres.Store
	Sessions   *SessionService
	Privy      privy.Client
	Jupiter    jupiter.Client
	XStocks    xstocks.Resolver
}

func integrationGovernanceApp(t *testing.T) governanceHarness {
	t.Helper()

	h := integrationApp(t)
	buy := NewBuyService(h.Jupiter, h.XStocks)
	governance := NewGovernanceService(h.Store, h.Privy)
	governance.SetBuyService(buy)
	return governanceHarness{
		Governance: governance,
		Store:      h.Store,
		Sessions:   NewSessionService(h.Store, h.Privy),
		Privy:      h.Privy,
		Jupiter:    h.Jupiter,
		XStocks:    h.XStocks,
	}
}

func seedUser(t *testing.T, sessions *SessionService, privyClient privy.Client, privyUserID, displayName string) string {
	t.Helper()

	token := privy.AccessToken("token-" + privyUserID)
	privy.RegisterToken(privyClient, token, privy.Identity{
		PrivyUserID: privyUserID,
		DisplayName: displayName,
	})
	result, err := sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return result.UserID
}

func registerRoutableQuote(t *testing.T, jupiterClient jupiter.Client, resolver xstocks.Resolver, symbol string, usdc int64) {
	t.Helper()

	mint := "Mint" + symbol
	xstocks.RegisterSolanaMint(resolver, symbol, mint)
	jupiter.RegisterQuoteBuy(jupiterClient, mint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: mint,
		InAmount:   strconv.FormatInt(usdc, 10),
		OutAmount:  strconv.FormatInt(usdc, 10),
	})
}

func TestPOST_proposals_happyPath_createsOpenProposalWithExpiry(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	token := privy.AccessToken("token-proposer")
	privy.RegisterToken(h.Privy, token, privy.Identity{PrivyUserID: "did:privy:proposer", DisplayName: "Proposer"})
	userID, err := h.Sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(token), "Vote Fund", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "AAPLx", 2_000_000)
	fixedNow := time.Unix(1_700_000_000, 0).UTC()
	h.Governance.SetClock(func() time.Time { return fixedNow })

	// Act
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "AAPLx",
		UsdcMicros: 2_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if proposal.Status != ProposalOpen {
		t.Fatalf("status = %q, want open", proposal.Status)
	}
	wantExpiry := fixedNow.Add(time.Duration(DefaultGroupRules().VoteExpirySeconds) * time.Second).Unix()
	if proposal.ExpiresAt != wantExpiry {
		t.Fatalf("expiresAt = %d, want %d", proposal.ExpiresAt, wantExpiry)
	}
	if proposal.Symbol != "AAPLx" || proposal.UsdcMicros != 2_000_000 {
		t.Fatalf("unexpected proposal payload: %+v", proposal)
	}
}

func TestTallyProposal_expiredOpenProposal_failsWithoutSwap(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	token := privy.AccessToken("token-expiry")
	privy.RegisterToken(h.Privy, token, privy.Identity{PrivyUserID: "did:privy:expiry", DisplayName: "Expiry"})
	userID, err := h.Sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	rules := DefaultGroupRules()
	rules.VoteExpirySeconds = 60
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(token), "Expiry Fund", rules, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "TSLAx", 1_000_000)
	start := time.Unix(1_700_100_000, 0).UTC()
	h.Governance.SetClock(func() time.Time { return start })
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "TSLAx",
		UsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	h.Governance.SetClock(func() time.Time { return start.Add(61 * time.Second) })

	// Act
	final, err := h.Governance.FinalizeExpiredProposal(context.Background(), proposal.ID)

	// Assert
	if err != nil {
		t.Fatalf("FinalizeExpiredProposal: %v", err)
	}
	if final.Status != ProposalExpired {
		t.Fatalf("status = %q, want expired", final.Status)
	}
	count, err := h.Store.CountTransactionsForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no swap transactions, got %d", count)
	}
}

func TestPOST_vote_nonVoterSetMember_returns403(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	creatorToken := privy.AccessToken("token-creator")
	privy.RegisterToken(h.Privy, creatorToken, privy.Identity{PrivyUserID: "did:privy:creator", DisplayName: "Creator"})
	creator, err := h.Sessions.OpenSession(context.Background(), string(creatorToken))
	if err != nil {
		t.Fatalf("creator session: %v", err)
	}
	rules := DefaultGroupRules()
	rules.VoterSet = VoterSet{Mode: VoterSetNamed, MemberIDs: []string{creator.UserID}}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(creatorToken), "Named Voters", rules, "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	outsiderID := seedUser(t, h.Sessions, h.Privy, "did:privy:outsider", "Outsider")
	tx, err := h.Store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := h.Store.InsertGroupMemberTx(context.Background(), tx, created.GroupID, outsiderID); err != nil {
		t.Fatalf("insert outsider member: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit member: %v", err)
	}

	registerRoutableQuote(t, h.Jupiter, h.XStocks, "NVDAx", 500_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: creator.UserID,
		Symbol:     "NVDAx",
		UsdcMicros: 500_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	_, err = h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    outsiderID,
		Choice:     domain.VoteYes,
	})

	// Assert
	if !errors.Is(err, ErrNotEligibleVoter) {
		t.Fatalf("err = %v, want ErrNotEligibleVoter (maps to HTTP 403)", err)
	}
	if httpStatusForVoteErr(err) != http.StatusForbidden {
		t.Fatalf("http status = %d, want 403", httpStatusForVoteErr(err))
	}
}

func TestPOST_vote_doubleVoteSameMember_isIdempotentOrRejected(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	token := privy.AccessToken("token-double")
	privy.RegisterToken(h.Privy, token, privy.Identity{PrivyUserID: "did:privy:double", DisplayName: "Double"})
	userID, err := h.Sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(token), "Double Vote", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "MSFTx", 750_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "MSFTx",
		UsdcMicros: 750_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	first, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("first vote: %v", err)
	}
	second, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID.UserID,
		Choice:     domain.VoteYes,
	})

	// Assert
	if err != nil {
		t.Fatalf("second vote should be idempotent: %v", err)
	}
	if second.Status != first.Status {
		t.Fatalf("status after duplicate vote = %q, want %q", second.Status, first.Status)
	}
	votes, err := h.Store.ListVotesForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("list votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("expected 1 ballot row, got %d", len(votes))
	}
}

func TestPOST_vote_concurrentDoubleVote_recordsOneBallot(t *testing.T) {
	// Arrange
	h := integrationGovernanceApp(t)
	token := privy.AccessToken("token-race")
	privy.RegisterToken(h.Privy, token, privy.Identity{PrivyUserID: "did:privy:race", DisplayName: "Race"})
	userID, err := h.Sessions.OpenSession(context.Background(), string(token))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	created, err := h.Governance.CreateGroupWithRules(context.Background(), string(token), "Race Vote", DefaultGroupRules(), "")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	registerRoutableQuote(t, h.Jupiter, h.XStocks, "GOOGx", 900_000)
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:    created.GroupID,
		ProposerID: userID.UserID,
		Symbol:     "GOOGx",
		UsdcMicros: 900_000,
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}

	// Act
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = h.Governance.CastVote(context.Background(), CastVoteInput{
				ProposalID: proposal.ID,
				VoterID:    userID.UserID,
				Choice:     domain.VoteYes,
			})
		}(i)
	}
	wg.Wait()

	// Assert
	for _, err := range errs {
		if err != nil && !errors.Is(err, ErrProposalNotOpen) {
			t.Fatalf("unexpected concurrent vote error: %v", err)
		}
	}
	votes, err := h.Store.ListVotesForProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("list votes: %v", err)
	}
	if len(votes) != 1 {
		t.Fatalf("expected 1 ballot row after race, got %d", len(votes))
	}
}

func httpStatusForVoteErr(err error) int {
	switch {
	case errors.Is(err, ErrNotEligibleVoter):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
