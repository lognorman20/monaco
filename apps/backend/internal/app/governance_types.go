package app

import (
	"time"

	"github.com/monaco/monaco/packages/domain"
)

type GroupRules = domain.GroupRules
type JoinPolicy = domain.JoinPolicy
type JoinMode = domain.JoinMode

const (
	JoinModeOpen    = domain.JoinModeOpen
	JoinModeRequest = domain.JoinModeRequest
)

type JoinGroupOutcome string

const (
	JoinOutcomeJoined        JoinGroupOutcome = "joined"
	JoinOutcomePending       JoinGroupOutcome = "pending"
	JoinOutcomeAlreadyMember JoinGroupOutcome = "already_member"
)

type JoinRequest struct {
	ID          string
	UserID      string
	DisplayName string
	RequestedAt time.Time
}

type VoterSet = domain.VoterSet
type VoterSetMode = domain.VoterSetMode

const (
	VoterSetAllMembers = domain.VoterSetAllMembers
	VoterSetNamed      = domain.VoterSetNamed
)

type VoteThreshold = domain.VoteThreshold

const (
	ThresholdUnanimous = domain.ThresholdUnanimous
	ThresholdMajority  = domain.ThresholdMajority
)

type VoteExpirySeconds = domain.VoteExpirySeconds
type ProposalStatus = domain.ProposalStatus

const (
	ProposalOpen    = domain.ProposalOpen
	ProposalPassed  = domain.ProposalPassed
	ProposalFailed  = domain.ProposalFailed
	ProposalExpired = domain.ProposalExpired
)

type WithdrawalStatus = domain.WithdrawalStatus
type Proposal = domain.Proposal
