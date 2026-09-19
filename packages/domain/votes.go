package domain

import "fmt"

// ProposalStatus is persisted on proposals.status.
type ProposalStatus string

const (
	ProposalOpen    ProposalStatus = "open"
	ProposalPassed  ProposalStatus = "passed"
	ProposalFailed  ProposalStatus = "failed"
	ProposalExpired ProposalStatus = "expired"
)

// ParseProposalStatus parses a proposals.status column value.
func ParseProposalStatus(raw string) (ProposalStatus, error) {
	switch ProposalStatus(raw) {
	case ProposalOpen, ProposalPassed, ProposalFailed, ProposalExpired:
		return ProposalStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid proposal status: %q", raw)
	}
}

// VoteChoice is persisted on votes.choice.
type VoteChoice string

const (
	VoteYes VoteChoice = "yes"
	VoteNo  VoteChoice = "no"
)

// ParseVoteChoice parses a votes.choice column value.
func ParseVoteChoice(raw string) (VoteChoice, error) {
	switch VoteChoice(raw) {
	case VoteYes, VoteNo:
		return VoteChoice(raw), nil
	default:
		return "", fmt.Errorf("invalid vote choice: %q", raw)
	}
}

// ValidateVoterSet returns an error when named subset mode has no voters.
func ValidateVoterSet(vs VoterSet) error {
	if vs.Mode == VoterSetNamed && len(vs.MemberIDs) < 1 {
		return fmt.Errorf("named voter subset requires at least one member")
	}
	return nil
}

// MemberMayVote reports whether memberID may cast a ballot under vs.
func MemberMayVote(vs VoterSet, memberID string, allMemberIDs []string) bool {
	switch vs.Mode {
	case VoterSetAllMembers:
		for _, id := range allMemberIDs {
			if id == memberID {
				return true
			}
		}
		return false
	case VoterSetNamed:
		for _, id := range vs.MemberIDs {
			if id == memberID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// Proposal is a buy or sell vote under consideration in a group.
type Proposal struct {
	ID          string
	GroupID     string
	ProposerID  string
	Symbol      string
	Kind        ProposalKind
	UsdcMicros  int64
	TokenAmount int64
	Status      ProposalStatus
	ExpiresAt   int64 // unix seconds
}

// ProposalKind is persisted on proposals.kind.
type ProposalKind string

const (
	ProposalKindBuy         ProposalKind = "buy"
	ProposalKindSell        ProposalKind = "sell"
	ProposalKindAddAgent    ProposalKind = "add_agent"
	ProposalKindPauseAgent  ProposalKind = "pause_agent"
	ProposalKindResumeAgent ProposalKind = "resume_agent"
	ProposalKindRevokeAgent ProposalKind = "revoke_agent"
)

// IsAgentGovernanceKind reports whether kind is an agent lifecycle vote.
func IsAgentGovernanceKind(kind ProposalKind) bool {
	switch kind {
	case ProposalKindAddAgent, ProposalKindPauseAgent, ProposalKindResumeAgent, ProposalKindRevokeAgent:
		return true
	default:
		return false
	}
}

// ParseProposalKind parses a proposals.kind column value.
func ParseProposalKind(raw string) (ProposalKind, error) {
	switch ProposalKind(raw) {
	case ProposalKindBuy, ProposalKindSell,
		ProposalKindAddAgent, ProposalKindPauseAgent, ProposalKindResumeAgent, ProposalKindRevokeAgent:
		return ProposalKind(raw), nil
	default:
		return "", fmt.Errorf("invalid proposal kind: %q", raw)
	}
}

// VoteTallyInput is pure input for off-chain proposal tally.
type VoteTallyInput struct {
	Threshold VoteThreshold
	VoterIDs  []string
	Votes     map[string]VoteChoice
	ExpiresAt int64 // unix seconds
	Now       int64 // unix seconds
	Status    ProposalStatus
}

// TallyProposal returns the next status for an open proposal from cast ballots.
func TallyProposal(in VoteTallyInput) (ProposalStatus, error) {
	if in.Status != ProposalOpen {
		return in.Status, nil
	}
	if in.Now >= in.ExpiresAt {
		return ProposalExpired, nil
	}

	voterSet := make(map[string]struct{}, len(in.VoterIDs))
	for _, id := range in.VoterIDs {
		voterSet[id] = struct{}{}
	}

	yes := 0
	no := 0
	voted := 0
	for voterID, choice := range in.Votes {
		if _, ok := voterSet[voterID]; !ok {
			continue
		}
		voted++
		switch choice {
		case VoteYes:
			yes++
		case VoteNo:
			no++
		default:
			return "", fmt.Errorf("invalid vote choice for voter %q", voterID)
		}
	}

	remaining := len(in.VoterIDs) - voted

	switch in.Threshold {
	case ThresholdUnanimous:
		if no > 0 {
			return ProposalFailed, nil
		}
		if voted == len(in.VoterIDs) && yes == len(in.VoterIDs) {
			return ProposalPassed, nil
		}
		return ProposalOpen, nil
	case ThresholdMajority:
		if yes > no+remaining {
			return ProposalPassed, nil
		}
		if no >= yes+remaining {
			return ProposalFailed, nil
		}
		return ProposalOpen, nil
	default:
		return "", fmt.Errorf("invalid threshold: %q", in.Threshold)
	}
}
