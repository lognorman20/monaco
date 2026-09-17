package domain

import "testing"

func TestVoterSet_namedSubset_enforcesMinimumSizeOne(t *testing.T) {
	// Arrange
	vs := VoterSet{Mode: VoterSetNamed, MemberIDs: []string{}}

	// Act
	err := ValidateVoterSet(vs)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty named voter subset")
	}
}

func TestTallyProposal_unanimous_allYes_passes(t *testing.T) {
	// Arrange
	input := VoteTallyInput{
		Threshold: ThresholdUnanimous,
		VoterIDs:  []string{"a", "b", "c"},
		Votes: map[string]VoteChoice{
			"a": VoteYes,
			"b": VoteYes,
			"c": VoteYes,
		},
		ExpiresAt: 1_700_000_100,
		Now:       1_700_000_000,
		Status:    ProposalOpen,
	}

	// Act
	status, err := TallyProposal(input)

	// Assert
	if err != nil {
		t.Fatalf("TallyProposal: %v", err)
	}
	if status != ProposalPassed {
		t.Fatalf("status = %q, want passed", status)
	}
}

func TestTallyProposal_majority_moreYesThanNo_passes(t *testing.T) {
	// Arrange
	input := VoteTallyInput{
		Threshold: ThresholdMajority,
		VoterIDs:  []string{"a", "b", "c"},
		Votes: map[string]VoteChoice{
			"a": VoteYes,
			"b": VoteYes,
			"c": VoteNo,
		},
		ExpiresAt: 1_700_000_100,
		Now:       1_700_000_000,
		Status:    ProposalOpen,
	}

	// Act
	status, err := TallyProposal(input)

	// Assert
	if err != nil {
		t.Fatalf("TallyProposal: %v", err)
	}
	if status != ProposalPassed {
		t.Fatalf("status = %q, want passed", status)
	}
}

func TestTallyProposal_majority_moreNoThanYes_fails(t *testing.T) {
	// Arrange
	input := VoteTallyInput{
		Threshold: ThresholdMajority,
		VoterIDs:  []string{"a", "b", "c"},
		Votes: map[string]VoteChoice{
			"a": VoteYes,
			"b": VoteNo,
			"c": VoteNo,
		},
		ExpiresAt: 1_700_000_100,
		Now:       1_700_000_000,
		Status:    ProposalOpen,
	}

	// Act
	status, err := TallyProposal(input)

	// Assert
	if err != nil {
		t.Fatalf("TallyProposal: %v", err)
	}
	if status != ProposalFailed {
		t.Fatalf("status = %q, want failed", status)
	}
}

func TestVoterSet_everyMemberMode_allowsAllMembersToVote(t *testing.T) {
	// Arrange
	vs := VoterSet{Mode: VoterSetAllMembers}
	memberIDs := []string{"user-a", "user-b", "user-c"}

	// Act
	mayVoteA := MemberMayVote(vs, "user-a", memberIDs)
	mayVoteB := MemberMayVote(vs, "user-b", memberIDs)
	mayVoteOutsider := MemberMayVote(vs, "user-z", memberIDs)

	// Assert
	if !mayVoteA || !mayVoteB {
		t.Fatal("expected every member to be allowed to vote")
	}
	if mayVoteOutsider {
		t.Fatal("expected non-member to be denied")
	}
}
