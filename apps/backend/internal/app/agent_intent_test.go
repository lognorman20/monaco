package app

// #202 demo coverage: the key is shown to the proposer exactly once, a buy intent within
// budget executes end to end through the fake Jupiter/Privy providers, a second intent that
// would breach the allocation is rejected without touching the swap path, and the
// pause/resume/revoke lifecycle gates intents the way the operator runbook promises.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/packages/domain"
)

// registerAgentBuyFill wires a routable quote AND a confirmed execute-poll fill for an agent
// buy, so DevExecuteBuy runs to completion against the fake Jupiter client instead of failing
// on "missing fill amount" (the default fake execute result carries no fill amounts). label
// must be unique per call (e.g. the test isolation suffix) so its execute request id and
// tx_signature never collide with another test's confirmed transaction in the shared test DB.
func registerAgentBuyFill(t *testing.T, jupiterClient dex.Client, resolver b20.Catalog, label, symbol string, usdc int64) {
	t.Helper()
	mint := "Mint" + symbol
	b20.RegisterTokenAddress(resolver, symbol, mint)
	requestID := fmt.Sprintf("agent-buy-%s-%s-%d", label, symbol, usdc)
	jupiter.RegisterQuoteBuy(jupiterClient, mint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputToken: mint,
		InAmount:   strconv.FormatInt(usdc, 10),
		OutAmount:  strconv.FormatInt(usdc, 10),
		RequestID:  requestID,
	})
	jupiter.RegisterExecutePoll(jupiterClient, requestID, []jupiter.ExecuteResult{
		{
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			Signature:          "sig-" + requestID,
			InputAmountResult:  strconv.FormatInt(usdc, 10),
			OutputAmountResult: strconv.FormatInt(usdc, 10),
		},
	})
}

func addAgentAndReveal(t *testing.T, h governanceHarness, groupID, proposerID string, allocationMicros int64) (proposalID, key string) {
	t.Helper()
	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:              groupID,
		ProposerID:           proposerID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Scout",
		AllocationUsdcMicros: allocationMicros,
	})
	if err != nil {
		t.Fatalf("create add agent proposal: %v", err)
	}
	passed, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    proposerID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("cast vote (add agent): %v", err)
	}
	if passed.Status != ProposalPassed {
		t.Fatalf("add agent proposal status = %s, want passed", passed.Status)
	}
	key, ok, err := h.Governance.RevealAgentKeyForProposer(context.Background(), proposal.ID, proposerID, proposerID, passed.Status)
	if err != nil {
		t.Fatalf("RevealAgentKeyForProposer: %v", err)
	}
	if !ok || key == "" {
		t.Fatalf("expected key reveal, got ok=%v key=%q", ok, key)
	}
	return proposal.ID, key
}

func TestAgentKeyReveal_shownOnceToProposerOnly(t *testing.T) {
	h := integrationGovernanceApp(t)
	proposer := openTestSession(t, h.ISO, h.Sessions, h.Privy, "key-proposer", "Key Proposer")
	other := openTestSession(t, h.ISO, h.Sessions, h.Privy, "key-bystander", "Key Bystander")
	token := h.ISO.UniqueToken("key-proposer")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "keyreveal"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 5_000_000)

	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:              created.GroupID,
		ProposerID:           proposer.UserID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Scout",
		AllocationUsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("create add agent proposal: %v", err)
	}
	passed, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    proposer.UserID,
		Choice:     domain.VoteYes,
	})
	if err != nil {
		t.Fatalf("cast vote: %v", err)
	}

	// A bystander viewing the same passed proposal never sees the key, and their look does not
	// close the reveal window for the real proposer.
	bystanderKey, ok, err := h.Governance.RevealAgentKeyForProposer(context.Background(), proposal.ID, proposer.UserID, other.UserID, passed.Status)
	if err != nil {
		t.Fatalf("RevealAgentKeyForProposer(bystander): %v", err)
	}
	if ok || bystanderKey != "" {
		t.Fatalf("bystander got key ok=%v key=%q, want none", ok, bystanderKey)
	}

	key, ok, err := h.Governance.RevealAgentKeyForProposer(context.Background(), proposal.ID, proposer.UserID, proposer.UserID, passed.Status)
	if err != nil {
		t.Fatalf("RevealAgentKeyForProposer(proposer): %v", err)
	}
	if !ok || len(key) != 5 {
		t.Fatalf("expected a 5-char key on first reveal, got ok=%v key=%q", ok, key)
	}

	// The detail screen refetches (after a vote, on every poll), so a second read inside the
	// window must return the same key rather than lose it.
	again, ok, err := h.Governance.RevealAgentKeyForProposer(context.Background(), proposal.ID, proposer.UserID, proposer.UserID, passed.Status)
	if err != nil {
		t.Fatalf("RevealAgentKeyForProposer(second read): %v", err)
	}
	if !ok || again != key {
		t.Fatalf("second read inside the window = ok=%v key=%q, want the same key %q", ok, again, key)
	}

	// Once the window has passed the plaintext is purged, for the proposer too.
	expired, ok, err := h.Store.ReadAgentKeyReveal(context.Background(), proposal.ID, 0)
	if err != nil {
		t.Fatalf("ReadAgentKeyReveal(expired window): %v", err)
	}
	if ok || expired != "" {
		t.Fatalf("expired window returned ok=%v key=%q, want none", ok, expired)
	}
	gone, ok, err := h.Governance.RevealAgentKeyForProposer(context.Background(), proposal.ID, proposer.UserID, proposer.UserID, passed.Status)
	if err != nil {
		t.Fatalf("RevealAgentKeyForProposer(after purge): %v", err)
	}
	if ok || gone != "" {
		t.Fatalf("key still readable after purge: ok=%v key=%q", ok, gone)
	}
}

func TestAgentIntent_buyExecutesThenEnforcesBudgetCap(t *testing.T) {
	h := integrationGovernanceApp(t)
	proposer := openTestSession(t, h.ISO, h.Sessions, h.Privy, "intent-proposer", "Intent Proposer")
	token := h.ISO.UniqueToken("intent-proposer")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "intent"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerAgentBuyFill(t, h.Jupiter, h.XStocks, h.ISO.Suffix(), "AAPLx", 3_000_000)

	_, key := addAgentAndReveal(t, h, created.GroupID, proposer.UserID, 5_000_000)

	intents := NewAgentIntentService(h.Store, h.Swap, h.Symbols)

	// A $3 buy within the $5 budget executes through the fake Jupiter/Privy path.
	result, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID:    created.GroupID,
		AgentKey:   key,
		Side:       domain.AgentIntentBuy,
		Symbol:     "AAPLx",
		UsdcMicros: 3_000_000,
	})
	if err != nil {
		t.Fatalf("SubmitAgentIntent(buy): %v", err)
	}
	if result.Status != "executed" || result.TransactionID == "" {
		t.Fatalf("expected executed intent with a transaction id, got %+v", result)
	}

	// A second $3 buy only has $2 of allocation left: rejected before any swap is attempted.
	rejected, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID:    created.GroupID,
		AgentKey:   key,
		Side:       domain.AgentIntentBuy,
		Symbol:     "AAPLx",
		UsdcMicros: 3_000_000,
	})
	if !errors.Is(err, ErrAgentIntentRejected) {
		t.Fatalf("expected ErrAgentIntentRejected, got %v", err)
	}
	if rejected.Status != "rejected" || rejected.RejectReason == "" {
		t.Fatalf("expected a rejected result with a reason, got %+v", rejected)
	}

	// Bad key: unauthenticated (401-equivalent) regardless of budget.
	if _, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID:    created.GroupID,
		AgentKey:   "wrong",
		Side:       domain.AgentIntentBuy,
		Symbol:     "AAPLx",
		UsdcMicros: 1_000_000,
	}); !errors.Is(err, ErrInvalidAgentAPIKey) {
		t.Fatalf("expected ErrInvalidAgentAPIKey for a bad key, got %v", err)
	}
}

func TestAgentLifecycle_pauseBlocksIntentsResumeRestoresRevokeInvalidatesKey(t *testing.T) {
	h := integrationGovernanceApp(t)
	proposer := openTestSession(t, h.ISO, h.Sessions, h.Privy, "lifecycle-proposer", "Lifecycle Proposer")
	token := h.ISO.UniqueToken("lifecycle-proposer")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "lifecycle"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 10_000_000)
	registerAgentBuyFill(t, h.Jupiter, h.XStocks, h.ISO.Suffix(), "AAPLx", 1_000_000)

	_, key := addAgentAndReveal(t, h, created.GroupID, proposer.UserID, 5_000_000)
	intents := NewAgentIntentService(h.Store, h.Swap, h.Symbols)

	// Vote to pause: same key, but every intent now comes back as paused (403-equivalent).
	pauseProposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID: created.GroupID, ProposerID: proposer.UserID, Kind: domain.ProposalKindPauseAgent,
	})
	if err != nil {
		t.Fatalf("create pause proposal: %v", err)
	}
	if _, err := h.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: pauseProposal.ID, VoterID: proposer.UserID, Choice: domain.VoteYes}); err != nil {
		t.Fatalf("cast vote (pause): %v", err)
	}
	view, err := h.Governance.GetGroupAgentView(context.Background(), created.GroupID)
	if err != nil || view == nil || view.Status != domain.AgentStatusPaused {
		t.Fatalf("expected paused agent after vote, got %+v (err %v)", view, err)
	}
	if _, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID: created.GroupID, AgentKey: key, Side: domain.AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1_000_000,
	}); !errors.Is(err, ErrAgentPaused) {
		t.Fatalf("expected ErrAgentPaused while paused, got %v", err)
	}

	// Vote to resume: the same key starts working again without a new install.
	resumeProposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID: created.GroupID, ProposerID: proposer.UserID, Kind: domain.ProposalKindResumeAgent,
	})
	if err != nil {
		t.Fatalf("create resume proposal: %v", err)
	}
	if _, err := h.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: resumeProposal.ID, VoterID: proposer.UserID, Choice: domain.VoteYes}); err != nil {
		t.Fatalf("cast vote (resume): %v", err)
	}
	result, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID: created.GroupID, AgentKey: key, Side: domain.AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1_000_000,
	})
	if err != nil || result.Status != "executed" {
		t.Fatalf("expected executed intent after resume, got %+v (err %v)", result, err)
	}

	// Vote to revoke: the key stops authenticating entirely (401-equivalent), not just paused.
	revokeProposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID: created.GroupID, ProposerID: proposer.UserID, Kind: domain.ProposalKindRevokeAgent,
	})
	if err != nil {
		t.Fatalf("create revoke proposal: %v", err)
	}
	if _, err := h.Governance.CastVote(context.Background(), CastVoteInput{ProposalID: revokeProposal.ID, VoterID: proposer.UserID, Choice: domain.VoteYes}); err != nil {
		t.Fatalf("cast vote (revoke): %v", err)
	}
	if _, err := intents.SubmitAgentIntent(context.Background(), SubmitAgentIntentInput{
		GroupID: created.GroupID, AgentKey: key, Side: domain.AgentIntentBuy, Symbol: "AAPLx", UsdcMicros: 1_000_000,
	}); !errors.Is(err, ErrInvalidAgentAPIKey) {
		t.Fatalf("expected ErrInvalidAgentAPIKey after revoke, got %v", err)
	}
}
