package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/packages/domain"
)

func TestValidateAgentProposal_addAgentRejectsDuplicate(t *testing.T) {
	h := integrationGovernanceApp(t)
	session := openTestSession(t, h.ISO, h.Sessions, h.Privy, "agent-proposer", "Agent Proposer")
	userID := session.UserID
	token := h.ISO.UniqueToken("agent-proposer")
	created, err := h.Governance.CreateGroupWithRules(context.Background(), token, testGroupName(h.ISO, "agent"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(created.GroupID)
	seedTestTreasuryUSDC(t, h.Privy, created.TreasuryAddress, 5_000_000)

	proposal, err := h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:              created.GroupID,
		ProposerID:           userID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Alpha",
		AllocationUsdcMicros: 1_000_000,
	})
	if err != nil {
		t.Fatalf("create add agent proposal: %v", err)
	}

	if _, err := h.Governance.CastVote(context.Background(), CastVoteInput{
		ProposalID: proposal.ID,
		VoterID:    userID,
		Choice:     domain.VoteYes,
	}); err != nil {
		t.Fatalf("cast vote: %v", err)
	}

	agentView, err := h.Governance.GetGroupAgentView(context.Background(), created.GroupID)
	if err != nil {
		t.Fatalf("get agent view: %v", err)
	}
	if agentView == nil || agentView.Status != domain.AgentStatusActive {
		t.Fatalf("expected active agent, got %+v", agentView)
	}

	_, err = h.Governance.CreateProposal(context.Background(), CreateProposalInput{
		GroupID:              created.GroupID,
		ProposerID:           userID,
		Kind:                 domain.ProposalKindAddAgent,
		AgentDisplayName:     "Beta",
		AllocationUsdcMicros: 500_000,
	})
	if err == nil {
		t.Fatal("expected duplicate agent rejection")
	}
}

func TestAgentAPIKey_mintAndHash(t *testing.T) {
	plaintext, hash, prefix, err := MintAgentAPIKey()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if plaintext == "" || hash == "" || prefix == "" {
		t.Fatal("expected non-empty key material")
	}
	if HashAgentAPIKey(plaintext) != hash {
		t.Fatal("hash mismatch")
	}
}
