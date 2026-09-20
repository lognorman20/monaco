package domain

import (
	"fmt"
	"math/rand"
	"testing"
)

// randomBallots returns n voter ids and a random partial set of ballots from them.
func randomBallots(rng *rand.Rand, n int) ([]string, map[string]VoteChoice) {
	voterIDs := make([]string, n)
	votes := map[string]VoteChoice{}
	for i := range voterIDs {
		voterIDs[i] = fmt.Sprintf("voter-%d", i)
		switch rng.Intn(3) {
		case 0:
			votes[voterIDs[i]] = VoteYes
		case 1:
			votes[voterIDs[i]] = VoteNo
		}
	}
	return voterIDs, votes
}

func bothThresholds() []VoteThreshold {
	return []VoteThreshold{ThresholdMajority, ThresholdUnanimous}
}

func TestTallyProposal_anyVoterOrBallotOrder_sameOutcome(t *testing.T) {
	rng := newSeededRand(t, 23)
	for _, threshold := range bothThresholds() {
		for i := 0; i < 500; i++ {
			// Arrange
			voterIDs, votes := randomBallots(rng, 1+rng.Intn(9))
			base := buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold = threshold
				in.VoterIDs = voterIDs
				in.Votes = votes
			})
			want, err := TallyProposal(base)
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}

			for perm := 0; perm < 5; perm++ {
				// Act — shuffle the voter list and rebuild the ballot map in a new insertion order.
				shuffled := append([]string(nil), voterIDs...)
				rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
				reordered := make(map[string]VoteChoice, len(votes))
				for _, id := range shuffled {
					if choice, ok := votes[id]; ok {
						reordered[id] = choice
					}
				}
				got, err := TallyProposal(buildTallyInput(func(in *VoteTallyInput) {
					in.Threshold = threshold
					in.VoterIDs = shuffled
					in.Votes = reordered
				}))

				// Assert
				if err != nil {
					t.Fatalf("TallyProposal: %v", err)
				}
				if got != want {
					t.Fatalf("%s: outcome %q changed to %q after reordering voters=%v votes=%v", threshold, want, got, shuffled, votes)
				}
			}
		}
	}
}

// assertDecisionHolds exhaustively casts every yes/no combination for the voters
// who have not voted yet and fails if any of them changes a decided outcome.
func assertDecisionHolds(t *testing.T, in VoteTallyInput, decided ProposalStatus) {
	t.Helper()
	var remaining []string
	for _, id := range in.VoterIDs {
		if _, voted := in.Votes[id]; !voted {
			remaining = append(remaining, id)
		}
	}
	for mask := 0; mask < 1<<len(remaining); mask++ {
		votes := make(map[string]VoteChoice, len(in.VoterIDs))
		for id, choice := range in.Votes {
			votes[id] = choice
		}
		for bit, id := range remaining {
			if mask&(1<<bit) != 0 {
				votes[id] = VoteYes
			} else {
				votes[id] = VoteNo
			}
		}
		next := in
		next.Votes = votes
		got, err := TallyProposal(next)
		if err != nil {
			t.Fatalf("TallyProposal: %v", err)
		}
		if got != decided {
			t.Fatalf("%s: decided %q with votes %v, but later ballots %v flipped it to %q",
				in.Threshold, decided, in.Votes, votes, got)
		}
	}
}

func TestTallyProposal_decidedEarly_remainingVotersCannotFlipIt(t *testing.T) {
	rng := newSeededRand(t, 29)
	for _, threshold := range bothThresholds() {
		decidedCount := 0
		for i := 0; i < 1_500; i++ {
			// Arrange
			voterIDs, votes := randomBallots(rng, 1+rng.Intn(8))
			in := buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold = threshold
				in.VoterIDs = voterIDs
				in.Votes = votes
			})

			// Act
			status, err := TallyProposal(in)

			// Assert
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}
			if status == ProposalOpen {
				continue
			}
			decidedCount++
			assertDecisionHolds(t, in, status)
		}
		if decidedCount == 0 {
			t.Fatalf("%s: generator never produced a decided proposal", threshold)
		}
	}
}

func TestTallyProposal_stillOpen_bothOutcomesRemainReachable(t *testing.T) {
	// Completeness counterpart of early-decision soundness: "open" must mean the
	// remaining voters really can still pass it and really can still fail it.
	rng := newSeededRand(t, 31)
	for _, threshold := range bothThresholds() {
		for i := 0; i < 1_000; i++ {
			// Arrange
			voterIDs, votes := randomBallots(rng, 1+rng.Intn(8))
			in := buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold = threshold
				in.VoterIDs = voterIDs
				in.Votes = votes
			})
			status, err := TallyProposal(in)
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}
			if status != ProposalOpen {
				continue
			}

			for _, fill := range []struct {
				choice VoteChoice
				want   ProposalStatus
			}{{VoteYes, ProposalPassed}, {VoteNo, ProposalFailed}} {
				// Act — every remaining voter casts the same ballot.
				filled := make(map[string]VoteChoice, len(voterIDs))
				for _, id := range voterIDs {
					filled[id] = fill.choice
				}
				for id, choice := range votes {
					filled[id] = choice
				}
				next := in
				next.Votes = filled
				got, err := TallyProposal(next)

				// Assert
				if err != nil {
					t.Fatalf("TallyProposal: %v", err)
				}
				if got != fill.want {
					t.Fatalf("%s: open with %v, remaining all-%s gave %q, want %q", threshold, votes, fill.choice, got, fill.want)
				}
			}
		}
	}
}

func TestTallyProposal_ballotsFromOutsideVoterSet_neverCount(t *testing.T) {
	rng := newSeededRand(t, 37)
	for _, threshold := range bothThresholds() {
		for i := 0; i < 1_000; i++ {
			// Arrange
			voterIDs, votes := randomBallots(rng, 1+rng.Intn(8))
			base := buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold = threshold
				in.VoterIDs = voterIDs
				in.Votes = votes
			})
			want, err := TallyProposal(base)
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}

			// Act — flood the ballot box with outsiders voting the same way.
			for _, choice := range []VoteChoice{VoteYes, VoteNo} {
				stuffed := make(map[string]VoteChoice, len(votes)+20)
				for id, c := range votes {
					stuffed[id] = c
				}
				for n := 0; n < 20; n++ {
					stuffed[fmt.Sprintf("outsider-%d", n)] = choice
				}
				next := base
				next.Votes = stuffed
				got, err := TallyProposal(next)

				// Assert
				if err != nil {
					t.Fatalf("TallyProposal: %v", err)
				}
				if got != want {
					t.Fatalf("%s: 20 outsider %s ballots moved outcome from %q to %q", threshold, choice, want, got)
				}
			}
		}
	}
}

func TestTallyProposal_onlyOutsidersVote_staysOpen(t *testing.T) {
	for _, threshold := range bothThresholds() {
		// Arrange
		in := buildTallyInput(func(in *VoteTallyInput) {
			in.Threshold = threshold
			in.VoterIDs = []string{"alex", "blair", "casey"}
			in.Votes = map[string]VoteChoice{"mallory": VoteYes, "trudy": VoteYes, "eve": VoteYes, "oscar": VoteYes}
		})

		// Act
		status, err := TallyProposal(in)

		// Assert
		if err != nil {
			t.Fatalf("TallyProposal: %v", err)
		}
		if status != ProposalOpen {
			t.Fatalf("%s: status = %q, want %q", threshold, status, ProposalOpen)
		}
	}
}

func TestTallyProposal_closedOrExpired_ignoresBallots(t *testing.T) {
	rng := newSeededRand(t, 41)
	for i := 0; i < 500; i++ {
		// Arrange
		voterIDs, votes := randomBallots(rng, 1+rng.Intn(6))
		closed := []ProposalStatus{ProposalPassed, ProposalFailed, ProposalExpired}[rng.Intn(3)]
		threshold := bothThresholds()[rng.Intn(2)]

		// Act
		closedStatus, errClosed := TallyProposal(buildTallyInput(func(in *VoteTallyInput) {
			in.Threshold, in.VoterIDs, in.Votes, in.Status = threshold, voterIDs, votes, closed
		}))
		expiredStatus, errExpired := TallyProposal(buildTallyInput(func(in *VoteTallyInput) {
			in.Threshold, in.VoterIDs, in.Votes = threshold, voterIDs, votes
			in.Now = in.ExpiresAt + rng.Int63n(1_000)
		}))

		// Assert
		if errClosed != nil || errExpired != nil {
			t.Fatalf("TallyProposal: %v / %v", errClosed, errExpired)
		}
		if closedStatus != closed {
			t.Fatalf("closed proposal %q re-tallied to %q", closed, closedStatus)
		}
		if expiredStatus != ProposalExpired {
			t.Fatalf("expired proposal tallied to %q", expiredStatus)
		}
	}
}

func TestTallyProposal_emptyVoterSet_neverPasses(t *testing.T) {
	for _, threshold := range bothThresholds() {
		// Arrange — nobody is eligible, so "everyone voted yes" is only vacuously true.
		in := buildTallyInput(func(in *VoteTallyInput) {
			in.Threshold = threshold
			in.Votes = map[string]VoteChoice{"outsider": VoteYes}
		})

		// Act
		status, err := TallyProposal(in)

		// Assert
		if err != nil {
			t.Fatalf("TallyProposal: %v", err)
		}
		if status != ProposalFailed {
			t.Fatalf("%s: status = %q, want %q", threshold, status, ProposalFailed)
		}
	}
}

func TestTallyProposal_duplicateVoterIDs_sameOutcomeAsDeduplicated(t *testing.T) {
	rng := newSeededRand(t, 43)
	for _, threshold := range bothThresholds() {
		for i := 0; i < 1_000; i++ {
			// Arrange
			voterIDs, votes := randomBallots(rng, 1+rng.Intn(8))
			want, err := TallyProposal(buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold, in.VoterIDs, in.Votes = threshold, voterIDs, votes
			}))
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}
			duplicated := append([]string(nil), voterIDs...)
			for n := rng.Intn(4) + 1; n > 0; n-- {
				duplicated = append(duplicated, voterIDs[rng.Intn(len(voterIDs))])
			}

			// Act
			got, err := TallyProposal(buildTallyInput(func(in *VoteTallyInput) {
				in.Threshold, in.VoterIDs, in.Votes = threshold, duplicated, votes
			}))

			// Assert — one member is one ballot however many times the id is listed.
			if err != nil {
				t.Fatalf("TallyProposal: %v", err)
			}
			if got != want {
				t.Fatalf("%s: voters %v votes %v gave %q, deduplicated gave %q", threshold, duplicated, votes, got, want)
			}
		}
	}
}
