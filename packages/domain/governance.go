package domain

import "fmt"

// JoinMode is persisted on groups.join_mode.
type JoinMode string

const (
	JoinModeOpen     JoinMode = "open"
	JoinModePassword JoinMode = "password"
)

// ParseJoinMode parses a groups.join_mode column value.
func ParseJoinMode(raw string) (JoinMode, error) {
	switch JoinMode(raw) {
	case JoinModeOpen, JoinModePassword:
		return JoinMode(raw), nil
	default:
		return "", fmt.Errorf("invalid join_mode: %q", raw)
	}
}

// JoinPolicy is how new members may join a group.
// Password groups store a hash in groups.join_password_hash.
type JoinPolicy struct {
	Mode         JoinMode
	PasswordHash string
}

// VoterSetMode is persisted on groups.voter_set_mode.
type VoterSetMode string

const (
	VoterSetAllMembers VoterSetMode = "all_members"
	VoterSetNamed      VoterSetMode = "named_subset"
)

// ParseVoterSetMode parses a groups.voter_set_mode column value.
func ParseVoterSetMode(raw string) (VoterSetMode, error) {
	switch VoterSetMode(raw) {
	case VoterSetAllMembers, VoterSetNamed:
		return VoterSetMode(raw), nil
	default:
		return "", fmt.Errorf("invalid voter_set_mode: %q", raw)
	}
}

// VoterSet names who may vote on proposals.
// Named subset rows live in group_voters when Mode is VoterSetNamed.
type VoterSet struct {
	Mode      VoterSetMode
	MemberIDs []string
}

// VoteThreshold is persisted on groups.threshold.
type VoteThreshold string

const (
	ThresholdUnanimous VoteThreshold = "unanimous"
	ThresholdMajority  VoteThreshold = "majority"
)

// ParseVoteThreshold parses a groups.threshold column value.
func ParseVoteThreshold(raw string) (VoteThreshold, error) {
	switch VoteThreshold(raw) {
	case ThresholdUnanimous, ThresholdMajority:
		return VoteThreshold(raw), nil
	default:
		return "", fmt.Errorf("invalid threshold: %q", raw)
	}
}

// VoteExpirySeconds is the default proposal voting window on a group.
// Persisted as groups.vote_expiry_seconds.
type VoteExpirySeconds int64

// GroupRules are the governance settings stored on a group row.
type GroupRules struct {
	JoinPolicy        JoinPolicy
	VoterSet          VoterSet
	Threshold         VoteThreshold
	VoteExpirySeconds VoteExpirySeconds
}
