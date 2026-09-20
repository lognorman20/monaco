package app

import (
	"context"
	"math"
	"strings"
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
	secret, ok := strings.CutPrefix(plaintext, AgentKeyPrefix)
	if !ok || len(secret) != agentKeySecretLength {
		t.Fatalf("expected %q + %d chars, got %q", AgentKeyPrefix, agentKeySecretLength, plaintext)
	}
	// At least 128 bits of crypto/rand: log2(31) * 32 chars is ~158.
	if bits := float64(agentKeySecretLength) * math.Log2(float64(len(agentKeyAlphabet))); bits < 128 {
		t.Fatalf("key carries %.1f bits, want >= 128", bits)
	}
	if !IsCurrentAgentKeyFormat(plaintext) {
		t.Fatalf("minted key %q is not recognised as the current format", plaintext)
	}
	// The stored prefix tells keys apart without being the key.
	if !strings.HasPrefix(plaintext, prefix) || len(prefix) >= len(plaintext)/2 {
		t.Fatalf("prefix %q should be a short head of the key", prefix)
	}
	for _, c := range secret {
		if !containsRune(agentKeyAlphabet, c) {
			t.Fatalf("key contains ambiguous or invalid char %q in %q", c, plaintext)
		}
	}
	another, _, _, err := MintAgentAPIKey()
	if err != nil {
		t.Fatalf("mint again: %v", err)
	}
	if another == plaintext {
		t.Fatal("two mints returned the same key")
	}
	if HashAgentAPIKey(plaintext) != hash {
		t.Fatal("hash mismatch")
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
