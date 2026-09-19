package domain

import "testing"

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
		ID:         "proposal-1",
		GroupID:    "group-1",
		ProposerID: "user-1",
		Symbol:     "AAPLx",
		UsdcMicros:  180_000,
		Kind:        ProposalKindBuy,
		TokenAmount: 0,
		Status:      ProposalOpen,
		ExpiresAt:  1_700_000_000,
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
