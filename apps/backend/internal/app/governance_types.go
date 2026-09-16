package app

import (
	"github.com/monaco/monaco/packages/domain"
)

// GroupRules is the governance settings for a group.
type GroupRules = domain.GroupRules

// JoinPolicy controls how members join a group.
type JoinPolicy = domain.JoinPolicy

// JoinMode is persisted on groups.join_mode.
type JoinMode = domain.JoinMode

const (
	JoinModeOpen     = domain.JoinModeOpen
	JoinModePassword = domain.JoinModePassword
)

// VoterSet names who may vote on proposals.
type VoterSet = domain.VoterSet

// VoterSetMode is persisted on groups.voter_set_mode.
type VoterSetMode = domain.VoterSetMode

const (
	VoterSetAllMembers = domain.VoterSetAllMembers
	VoterSetNamed      = domain.VoterSetNamed
)

// VoteThreshold is persisted on groups.threshold.
type VoteThreshold = domain.VoteThreshold

const (
	ThresholdUnanimous = domain.ThresholdUnanimous
	ThresholdMajority  = domain.ThresholdMajority
)

// VoteExpirySeconds is persisted on groups.vote_expiry_seconds.
type VoteExpirySeconds = domain.VoteExpirySeconds

// ProposalStatus is persisted on proposals.status.
type ProposalStatus = domain.ProposalStatus

const (
	ProposalOpen    = domain.ProposalOpen
	ProposalPassed  = domain.ProposalPassed
	ProposalFailed  = domain.ProposalFailed
	ProposalExpired = domain.ProposalExpired
)

// WithdrawalStatus is persisted on withdrawals.status.
type WithdrawalStatus = domain.WithdrawalStatus

// Proposal is a buy vote under consideration in a group.
type Proposal = domain.Proposal
