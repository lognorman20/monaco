package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

func TestAgentAPIKey_memberCanAlwaysReadFromGroupView(t *testing.T) {
	t.Parallel()
	h := integrationGovernanceApp(t)
	ctx := context.Background()
	session := openTestSession(t, h.ISO, h.Sessions, h.Privy, "agent-key-member", "Agent Key Member")
	userID := session.UserID
	token := h.ISO.UniqueToken("agent-key-member")
	created, err := h.Governance.CreateGroupWithRules(ctx, token, testGroupName(h.ISO, "agent-key"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 5_000_000)

	proposal, err := h.Governance.CreateProposal(ctx, CreateProposalInput{
		GroupID:              created.GroupID,
		ProposerID:           userID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Scout",
		AllocationUsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("create add agent proposal: %v", err)
	}
	if _, err := h.Governance.CastVote(ctx, CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID,
		Choice:     domain.VoteYes,
	}); err != nil {
		t.Fatalf("cast vote: %v", err)
	}

	detail1, err := h.Governance.GetProposalDetail(ctx, string(token), proposal.ID)
	if err != nil {
		t.Fatalf("get proposal detail 1: %v", err)
	}
	if detail1.MintedAgentKey == "" {
		t.Fatal("expected minted agent key on first proposal detail read")
	}

	detail2, err := h.Governance.GetProposalDetail(ctx, string(token), proposal.ID)
	if err != nil {
		t.Fatalf("get proposal detail 2: %v", err)
	}
	if detail2.MintedAgentKey != detail1.MintedAgentKey {
		t.Fatalf("expected same key on repeat read, got %q then %q", detail1.MintedAgentKey, detail2.MintedAgentKey)
	}

	agentView, err := h.Governance.GetGroupAgentViewForMember(ctx, created.GroupID, userID)
	if err != nil {
		t.Fatalf("get agent view: %v", err)
	}
	if agentView == nil || agentView.APIKey != detail1.MintedAgentKey {
		t.Fatalf("expected agent api key for member, got %+v", agentView)
	}

	outsider := openTestSession(t, h.ISO, h.Sessions, h.Privy, "agent-key-outsider", "Outsider")
	outsiderView, err := h.Governance.GetGroupAgentViewForMember(ctx, created.GroupID, outsider.UserID)
	if err != nil {
		t.Fatalf("get agent view for outsider: %v", err)
	}
	if outsiderView != nil && outsiderView.APIKey != "" {
		t.Fatalf("expected no api key for non-member, got %+v", outsiderView)
	}
}
