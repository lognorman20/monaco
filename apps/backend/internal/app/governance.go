package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
	"golang.org/x/crypto/bcrypt"
)

type GovernanceService struct {
	store *postgres.Store
	privy privy.Client
	buy   *BuyService
	now   func() time.Time
}

func NewGovernanceService(store *postgres.Store, privyClient privy.Client) *GovernanceService {
	return &GovernanceService{store: store, privy: privyClient, now: time.Now}
}

// SetBuyService wires quote gating for proposal create (M4-T13).
func (g *GovernanceService) SetBuyService(buy *BuyService) {
	g.buy = buy
}

// SetClock overrides time.Now for tests.
func (g *GovernanceService) SetClock(now func() time.Time) {
	if now == nil {
		g.now = time.Now
		return
	}
	g.now = now
}

var ErrInvalidGroupRules = errors.New("invalid group rules")
var ErrWrongJoinPassword = errors.New("wrong join password")
var ErrProposalNotFound = errors.New("proposal not found")
var ErrProposalNotOpen = errors.New("proposal not open")
var ErrNotEligibleVoter = errors.New("not eligible to vote")
var ErrNotEligibleProposer = errors.New("not eligible to propose")

// CreateProposalInput is input for buy proposal create (M4-T13).
type CreateProposalInput struct {
	GroupID    string
	ProposerID string
	Symbol     string
	UsdcMicros int64
}

// CastVoteInput is input for yes/no vote cast (M4-T14).
type CastVoteInput struct {
	ProposalID string
	VoterID    string
	Choice     domain.VoteChoice
}

func DefaultGroupRules() GroupRules {
	return GroupRules{
		JoinPolicy:        JoinPolicy{Mode: JoinModeOpen},
		VoterSet:          VoterSet{Mode: VoterSetAllMembers},
		Threshold:         ThresholdMajority,
		VoteExpirySeconds: 86_400,
	}
}

func (g *GovernanceService) CreateGroupWithRules(ctx context.Context, accessToken, name string, rules GroupRules, joinPassword string) (CreateGroupResult, error) {
	if name == "" {
		return CreateGroupResult{}, fmt.Errorf("name is required")
	}
	if err := validateCreateRules(rules, joinPassword); err != nil {
		return CreateGroupResult{}, err
	}
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return CreateGroupResult{}, privy.ErrInvalidToken
		}
		return CreateGroupResult{}, fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return CreateGroupResult{}, err
	}
	if !found {
		return CreateGroupResult{}, ErrUserNotFound
	}
	passwordHash, err := hashJoinPassword(rules.JoinPolicy.Mode, joinPassword)
	if err != nil {
		return CreateGroupResult{}, err
	}
	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return CreateGroupResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	group, err := g.store.InsertGroupWithRulesTx(ctx, tx, name, user.ID, rules, passwordHash)
	if err != nil {
		return CreateGroupResult{}, err
	}
	if err := g.store.InsertGroupMemberTx(ctx, tx, group.ID, user.ID); err != nil {
		return CreateGroupResult{}, err
	}
	if rules.VoterSet.Mode == VoterSetNamed {
		if err := g.store.InsertGroupVotersTx(ctx, tx, group.ID, rules.VoterSet.MemberIDs); err != nil {
			return CreateGroupResult{}, err
		}
	}
	treasuryRef, err := g.privy.EnsureTreasury(ctx, privy.GroupID(group.ID))
	if err != nil {
		return CreateGroupResult{}, fmt.Errorf("privy ensure treasury: %w", err)
	}
	if _, err = g.store.InsertTreasuryTx(ctx, tx, group.ID, treasuryRef.PrivyWalletID, treasuryRef.SolanaAddress); err != nil {
		return CreateGroupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreateGroupResult{}, fmt.Errorf("commit create group: %w", err)
	}
	committed = true
	return CreateGroupResult{GroupID: group.ID, Name: group.Name, TreasuryAddress: treasuryRef.SolanaAddress}, nil
}

func (g *GovernanceService) JoinGroup(ctx context.Context, accessToken, groupID, password string) error {
	if groupID == "" {
		return fmt.Errorf("group id is required")
	}
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return privy.ErrInvalidToken
		}
		return fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return err
	}
	if !found {
		return ErrUserNotFound
	}
	rules, found, err := g.store.GetGroupRules(ctx, groupID)
	if err != nil {
		return err
	}
	if !found {
		return ErrGroupNotFound
	}
	if err := verifyJoinPassword(rules.JoinPolicy, password); err != nil {
		return err
	}
	alreadyMember, err := g.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return err
	}
	if alreadyMember {
		return nil
	}
	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := g.store.InsertGroupMemberTx(ctx, tx, groupID, user.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit join group: %w", err)
	}
	committed = true
	return nil
}

func validateCreateRules(rules GroupRules, joinPassword string) error {
	if rules.VoteExpirySeconds <= 0 {
		return fmt.Errorf("%w: vote expiry must be positive", ErrInvalidGroupRules)
	}
	if rules.JoinPolicy.Mode == JoinModePassword && joinPassword == "" {
		return fmt.Errorf("%w: password required for password join mode", ErrInvalidGroupRules)
	}
	if err := domain.ValidateVoterSet(rules.VoterSet); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGroupRules, err)
	}
	switch rules.Threshold {
	case ThresholdUnanimous, ThresholdMajority:
	default:
		return fmt.Errorf("%w: invalid threshold", ErrInvalidGroupRules)
	}
	switch rules.JoinPolicy.Mode {
	case JoinModeOpen, JoinModePassword:
	default:
		return fmt.Errorf("%w: invalid join mode", ErrInvalidGroupRules)
	}
	switch rules.VoterSet.Mode {
	case VoterSetAllMembers, VoterSetNamed:
	default:
		return fmt.Errorf("%w: invalid voter set mode", ErrInvalidGroupRules)
	}
	return nil
}

func hashJoinPassword(mode JoinMode, joinPassword string) (string, error) {
	if mode != JoinModePassword {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(joinPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash join password: %w", err)
	}
	return string(hash), nil
}

func verifyJoinPassword(policy JoinPolicy, password string) error {
	switch policy.Mode {
	case JoinModeOpen:
		return nil
	case JoinModePassword:
		if policy.PasswordHash == "" {
			return fmt.Errorf("group join password not configured")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(policy.PasswordHash), []byte(password)); err != nil {
			return ErrWrongJoinPassword
		}
		return nil
	default:
		return fmt.Errorf("invalid join mode")
	}
}

// CreateProposal inserts an open buy proposal when the quote is routable (M4-T13).
func (g *GovernanceService) CreateProposal(ctx context.Context, in CreateProposalInput) (Proposal, error) {
	if in.GroupID == "" || in.ProposerID == "" {
		return Proposal{}, fmt.Errorf("group_id and proposer_id are required")
	}
	if in.Symbol == "" {
		return Proposal{}, fmt.Errorf("symbol is required")
	}
	if in.UsdcMicros <= 0 {
		return Proposal{}, fmt.Errorf("usdc must be positive")
	}
	if g.buy == nil {
		return Proposal{}, fmt.Errorf("buy service is required")
	}

	member, err := g.store.IsGroupMember(ctx, in.GroupID, in.ProposerID)
	if err != nil {
		return Proposal{}, err
	}
	if !member {
		return Proposal{}, ErrNotGroupMember
	}

	rules, found, err := g.store.GetGroupRules(ctx, in.GroupID)
	if err != nil {
		return Proposal{}, err
	}
	if !found {
		return Proposal{}, ErrGroupNotFound
	}

	voterSet, voterIDs, err := g.resolveVoterSet(ctx, in.GroupID, rules)
	if err != nil {
		return Proposal{}, err
	}
	if !domain.MemberMayVote(voterSet, in.ProposerID, voterIDs) {
		return Proposal{}, ErrNotEligibleProposer
	}

	_, err = g.buy.StartBuy(ctx, StartBuyRequest{
		GroupID:    in.GroupID,
		UserID:     in.ProposerID,
		Symbol:     in.Symbol,
		USDCAmount: in.UsdcMicros,
	})
	if err != nil {
		if errors.Is(err, ErrQuoteNotRoutable) {
			return Proposal{}, ErrQuoteNotRoutable
		}
		return Proposal{}, err
	}

	now := g.now().UTC()
	expiresAt := now.Add(time.Duration(rules.VoteExpirySeconds) * time.Second)

	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return Proposal{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	row, err := g.store.InsertProposalTx(ctx, tx, in.GroupID, in.ProposerID, in.Symbol, in.UsdcMicros, expiresAt)
	if err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proposal{}, fmt.Errorf("commit create proposal: %w", err)
	}
	committed = true

	return proposalFromRow(row), nil
}

// CastVote persists a ballot and tallies to passed, failed, or expired (M4-T14).
func (g *GovernanceService) CastVote(ctx context.Context, in CastVoteInput) (Proposal, error) {
	if in.ProposalID == "" || in.VoterID == "" {
		return Proposal{}, fmt.Errorf("proposal_id and voter_id are required")
	}
	switch in.Choice {
	case domain.VoteYes, domain.VoteNo:
	default:
		return Proposal{}, fmt.Errorf("invalid vote choice")
	}

	row, found, err := g.store.GetProposalByID(ctx, in.ProposalID)
	if err != nil {
		return Proposal{}, err
	}
	if !found {
		return Proposal{}, ErrProposalNotFound
	}
	proposal := proposalFromRow(row)

	_, foundVote, err := g.store.GetVoteByProposalAndVoter(ctx, in.ProposalID, in.VoterID)
	if err != nil {
		return Proposal{}, err
	}
	if foundVote {
		return proposal, nil
	}

	if proposal.Status == ProposalOpen && g.now().UTC().Unix() >= proposal.ExpiresAt {
		proposal, err = g.FinalizeExpiredProposal(ctx, in.ProposalID)
		if err != nil {
			return Proposal{}, err
		}
	}
	if proposal.Status != ProposalOpen {
		return proposal, ErrProposalNotOpen
	}

	rules, found, err := g.store.GetGroupRules(ctx, proposal.GroupID)
	if err != nil {
		return Proposal{}, err
	}
	if !found {
		return Proposal{}, ErrGroupNotFound
	}

	voterSet, voterIDs, err := g.resolveVoterSet(ctx, proposal.GroupID, rules)
	if err != nil {
		return Proposal{}, err
	}
	if !domain.MemberMayVote(voterSet, in.VoterID, voterIDs) {
		return Proposal{}, ErrNotEligibleVoter
	}

	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return Proposal{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, _, err := g.store.InsertVoteTx(ctx, tx, in.ProposalID, in.VoterID, in.Choice); err != nil {
		return Proposal{}, err
	}

	updated, err := g.tallyAndPersistTx(ctx, tx, proposal, rules, voterIDs)
	if err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proposal{}, fmt.Errorf("commit cast vote: %w", err)
	}
	committed = true
	return updated, nil
}

// FinalizeExpiredProposal marks an open past-deadline proposal expired with no swap (M4-T17).
func (g *GovernanceService) FinalizeExpiredProposal(ctx context.Context, proposalID string) (Proposal, error) {
	tx, err := g.store.BeginTx(ctx)
	if err != nil {
		return Proposal{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	proposal, err := g.finalizeOpenProposalTx(ctx, tx, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proposal{}, fmt.Errorf("commit finalize expired proposal: %w", err)
	}
	committed = true
	return proposal, nil
}

func (g *GovernanceService) resolveVoterSet(ctx context.Context, groupID string, rules GroupRules) (VoterSet, []string, error) {
	vs := rules.VoterSet
	switch vs.Mode {
	case VoterSetAllMembers:
		ids, err := g.store.ListGroupMemberIDs(ctx, groupID)
		if err != nil {
			return VoterSet{}, nil, err
		}
		return vs, ids, nil
	case VoterSetNamed:
		ids, err := g.store.ListGroupVoterIDs(ctx, groupID)
		if err != nil {
			return VoterSet{}, nil, err
		}
		vs.MemberIDs = ids
		return vs, ids, nil
	default:
		return VoterSet{}, nil, fmt.Errorf("invalid voter set mode")
	}
}

func (g *GovernanceService) finalizeOpenProposalTx(ctx context.Context, tx *sql.Tx, proposalID string) (Proposal, error) {
	row, found, err := g.store.GetProposalByIDTx(ctx, tx, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if !found {
		return Proposal{}, ErrProposalNotFound
	}
	proposal := proposalFromRow(row)
	if proposal.Status != ProposalOpen {
		return proposal, nil
	}
	if g.now().UTC().Unix() < proposal.ExpiresAt {
		return proposal, nil
	}

	ok, err := g.store.UpdateProposalStatusTx(ctx, tx, proposalID, ProposalOpen, ProposalExpired)
	if err != nil {
		return Proposal{}, err
	}
	if ok {
		proposal.Status = ProposalExpired
	}
	return proposal, nil
}

func (g *GovernanceService) tallyAndPersistTx(ctx context.Context, tx *sql.Tx, proposal Proposal, rules GroupRules, voterIDs []string) (Proposal, error) {
	if proposal.Status != ProposalOpen {
		return proposal, nil
	}

	votes, err := g.store.ListVotesForProposalTx(ctx, tx, proposal.ID)
	if err != nil {
		return Proposal{}, err
	}
	cast := make(map[string]domain.VoteChoice, len(votes))
	for _, vote := range votes {
		cast[vote.VoterID] = vote.Choice
	}

	nextStatus, err := domain.TallyProposal(domain.VoteTallyInput{
		Threshold: rules.Threshold,
		VoterIDs:  voterIDs,
		Votes:     cast,
		ExpiresAt: proposal.ExpiresAt,
		Now:       g.now().UTC().Unix(),
		Status:    proposal.Status,
	})
	if err != nil {
		return Proposal{}, err
	}
	if nextStatus == ProposalOpen {
		return proposal, nil
	}

	ok, err := g.store.UpdateProposalStatusTx(ctx, tx, proposal.ID, ProposalOpen, nextStatus)
	if err != nil {
		return Proposal{}, err
	}
	if ok {
		proposal.Status = nextStatus
	}
	return proposal, nil
}

func proposalFromRow(row postgres.ProposalRow) Proposal {
	return Proposal{
		ID:         row.ID,
		GroupID:    row.GroupID,
		ProposerID: row.ProposerID,
		Symbol:     row.Symbol,
		UsdcMicros: row.UsdcMicros,
		Status:     row.Status,
		ExpiresAt:  row.ExpiresAt.UTC().Unix(),
	}
}
