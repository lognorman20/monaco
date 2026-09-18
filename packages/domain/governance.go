package domain

import "fmt"

type JoinMode string

const (
	JoinModeOpen    JoinMode = "open"
	JoinModeRequest JoinMode = "request"
)

func ParseJoinMode(raw string) (JoinMode, error) {
	switch JoinMode(raw) {
	case JoinModeOpen, JoinModeRequest:
		return JoinMode(raw), nil
	default:
		return "", fmt.Errorf("invalid join_mode: %q", raw)
	}
}

type JoinPolicy struct {
	Mode JoinMode
}

type JoinRequestStatus string

const (
	JoinRequestPending  JoinRequestStatus = "pending"
	JoinRequestApproved JoinRequestStatus = "approved"
	JoinRequestDenied   JoinRequestStatus = "denied"
)

type VoterSetMode string

const (
	VoterSetAllMembers VoterSetMode = "all_members"
	VoterSetNamed      VoterSetMode = "named_subset"
)

func ParseVoterSetMode(raw string) (VoterSetMode, error) {
	switch VoterSetMode(raw) {
	case VoterSetAllMembers, VoterSetNamed:
		return VoterSetMode(raw), nil
	default:
		return "", fmt.Errorf("invalid voter_set_mode: %q", raw)
	}
}

type VoterSet struct {
	Mode      VoterSetMode
	MemberIDs []string
}

type VoteThreshold string

const (
	ThresholdUnanimous VoteThreshold = "unanimous"
	ThresholdMajority  VoteThreshold = "majority"
)

func ParseVoteThreshold(raw string) (VoteThreshold, error) {
	switch VoteThreshold(raw) {
	case ThresholdUnanimous, ThresholdMajority:
		return VoteThreshold(raw), nil
	default:
		return "", fmt.Errorf("invalid threshold: %q", raw)
	}
}

type VoteExpirySeconds int64

type GroupRules struct {
	JoinPolicy        JoinPolicy
	VoterSet          VoterSet
	Threshold         VoteThreshold
	VoteExpirySeconds VoteExpirySeconds
}
