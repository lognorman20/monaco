package domain

import (
	"math/rand"
	"testing"
)

func buildGroupWithRules(overrides func(*GroupRules)) GroupRules {
	rules := GroupRules{
		JoinPolicy: JoinPolicy{
			Mode: JoinModeOpen,
		},
		VoterSet: VoterSet{
			Mode: VoterSetAllMembers,
		},
		Threshold:         ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
	if overrides != nil {
		overrides(&rules)
	}
	return rules
}

func buildProposal(overrides func(*Proposal)) Proposal {
	proposal := Proposal{
		ID:          "proposal-1",
		GroupID:     "group-1",
		ProposerID:  "user-1",
		Symbol:      "AAPLx",
		UsdcMicros:  180_000,
		Kind:        ProposalKindBuy,
		TokenAmount: 0,
		Status:      ProposalOpen,
		ExpiresAt:   1_700_000_000,
	}
	if overrides != nil {
		overrides(&proposal)
	}
	return proposal
}

func TestBuildGroupWithRules_defaultsOpenMajority(t *testing.T) {
	// Arrange
	// Act
	rules := buildGroupWithRules(nil)

	// Assert
	if rules.JoinPolicy.Mode != JoinModeOpen {
		t.Fatalf("join mode: got %q want %q", rules.JoinPolicy.Mode, JoinModeOpen)
	}
	if rules.VoterSet.Mode != VoterSetAllMembers {
		t.Fatalf("voter set mode: got %q want %q", rules.VoterSet.Mode, VoterSetAllMembers)
	}
	if rules.Threshold != ThresholdMajority {
		t.Fatalf("threshold: got %q want %q", rules.Threshold, ThresholdMajority)
	}
}

func TestBuildProposal_defaultsOpenStatus(t *testing.T) {
	// Arrange
	// Act
	proposal := buildProposal(nil)

	// Assert
	if proposal.Status != ProposalOpen {
		t.Fatalf("status: got %q want %q", proposal.Status, ProposalOpen)
	}
	if proposal.Kind != ProposalKindBuy {
		t.Fatalf("kind: got %q want %q", proposal.Kind, ProposalKindBuy)
	}
	if proposal.TokenAmount != 0 {
		t.Fatalf("token amount: got %d want 0", proposal.TokenAmount)
	}
}

// buildActiveAgent returns an active agent with a $500 allocation.
func buildActiveAgent(overrides func(*GroupAgent)) GroupAgent {
	agent := GroupAgent{
		ID:                   "agent-1",
		GroupID:              "group-1",
		Status:               AgentStatusActive,
		AgentDisplayName:     "Test Agent",
		AllocationUsdcMicros: 500_000_000,
	}
	if overrides != nil {
		overrides(&agent)
	}
	return agent
}

// buildTreasurySnapshot returns a $1,000 treasury with nothing spent or pending.
func buildTreasurySnapshot(overrides func(*AgentTreasurySnapshot)) AgentTreasurySnapshot {
	snap := AgentTreasurySnapshot{
		TreasuryUsdcMicros:         1_000_000_000,
		TokenHoldingsBySymbol:      map[string]int64{"AAPLx": 1_000_000},
		AgentTokenHoldingsBySymbol: map[string]int64{"AAPLx": 1_000_000},
	}
	if overrides != nil {
		overrides(&snap)
	}
	return snap
}

// buildTallyInput returns an open, unexpired majority tally with no voters.
func buildTallyInput(overrides func(*VoteTallyInput)) VoteTallyInput {
	in := VoteTallyInput{
		Threshold: ThresholdMajority,
		Votes:     map[string]VoteChoice{},
		ExpiresAt: 1_700_000_000,
		Now:       1_600_000_000,
		Status:    ProposalOpen,
	}
	if overrides != nil {
		overrides(&in)
	}
	return in
}

// newSeededRand returns a deterministic generator so property failures reproduce.
func newSeededRand(t *testing.T, seed int64) *rand.Rand {
	t.Helper()
	t.Logf("property seed = %d", seed)
	return rand.New(rand.NewSource(seed))
}
