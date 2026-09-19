package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

type GovernanceService struct {
	store  *postgres.Store
	privy  privy.Client
	buy    *BuyService
	swap   *SwapService
	home   *HomeService
	redeem *RedeemService
	now    func() time.Time
}

func NewGovernanceService(store *postgres.Store, privyClient privy.Client) *GovernanceService {
	return &GovernanceService{store: store, privy: privyClient, now: time.Now}
}

// SetBuyService wires quote gating for proposal create (M4-T13).
func (g *GovernanceService) SetBuyService(buy *BuyService) {
	g.buy = buy
}

// SetSwapService wires treasury sell quotes for proposal create.
func (g *GovernanceService) SetSwapService(swap *SwapService) {
	g.swap = swap
}

// SetHomeService wires treasury total + reconcile for proposal create.
func (g *GovernanceService) SetHomeService(home *HomeService) {
	g.home = home
}

// SetRedeemService wires withdraw-to-balance for leave-with-stake.
func (g *GovernanceService) SetRedeemService(redeem *RedeemService) {
	g.redeem = redeem
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

type LeaveBlockReason string

const (
	LeaveBlockShareUnits          LeaveBlockReason = "share_units_remaining"
	LeaveBlockLastMemberTreasury  LeaveBlockReason = "last_member_with_treasury"
	LeaveBlockPendingRedeem       LeaveBlockReason = "pending_redeem"
	LeaveBlockSoleRemainingVote   LeaveBlockReason = "sole_remaining_vote"
	LeaveBlockCreatorMustTransfer LeaveBlockReason = "creator_must_transfer"
)

type LeaveGroupError struct{ Reason LeaveBlockReason }

func (e *LeaveGroupError) Error() string { return string(e.Reason) }

var ErrNotGroupMemberForLeave = errors.New("not a group member")
var ErrNotGroupAdmin = errors.New("not group admin")
var ErrJoinRequestNotFound = errors.New("join request not found")
var ErrProposalNotFound = errors.New("proposal not found")
var ErrProposalNotOpen = errors.New("proposal not open")
var ErrNotEligibleVoter = errors.New("not eligible to vote")
var ErrNotEligibleProposer = errors.New("not eligible to propose")
var ErrExceedsTreasuryUSDC = errors.New("exceeds treasury usdc")

// ErrExceedsTreasuryHolding means the sell amount is above confirmed treasury inventory.
var ErrExceedsTreasuryHolding = errors.New("exceeds treasury holding")

// CreateProposalInput is input for buy, sell, or agent lifecycle proposal create.
type CreateProposalInput struct {
	GroupID              string
	ProposerID           string
	Symbol               string
	Kind                 domain.ProposalKind
	UsdcMicros           int64
	TokenAmount          int64
	AgentDisplayName     string
	AllocationUsdcMicros int64
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

func (g *GovernanceService) CreateGroupWithRules(ctx context.Context, accessToken, name string, rules GroupRules) (CreateGroupResult, error) {
	if name == "" {
		logGovernanceBranchWarn("governance create group rejected", "name required")
		return CreateGroupResult{}, fmt.Errorf("name is required")
	}
	if err := validateCreateRules(rules); err != nil {
		logGovernanceBranchWarn("governance create group rejected", "invalid rules")
		return CreateGroupResult{}, err
	}
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			logGovernanceBranchWarn("governance create group rejected", "invalid token", "name", name)
			return CreateGroupResult{}, privy.ErrInvalidToken
		}
		logGovernanceBranchError("governance create group verify session failed", err, "name", name)
		return CreateGroupResult{}, fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		logGovernanceBranchError("governance create group lookup user failed", err, "name", name)
		return CreateGroupResult{}, err
	}
	if !found {
		logGovernanceBranchWarn("governance create group rejected", "user not found", "name", name)
		return CreateGroupResult{}, ErrUserNotFound
	}
	logGovernanceCreateGroupStart(user.ID, name)
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
	group, err := g.store.InsertGroupWithRulesTx(ctx, tx, name, user.ID, rules)
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
		logGovernanceBranchError("governance create group commit failed", err, "user_id", user.ID, "name", name)
		return CreateGroupResult{}, fmt.Errorf("commit create group: %w", err)
	}
	committed = true
	logGovernanceCreateGroupSuccess(group.ID, user.ID, name)
	return CreateGroupResult{GroupID: group.ID, Name: group.Name, TreasuryAddress: treasuryRef.SolanaAddress}, nil
}

func (g *GovernanceService) JoinGroup(ctx context.Context, accessToken, groupID string) (JoinGroupOutcome, error) {
	if groupID == "" {
		return "", fmt.Errorf("group id is required")
	}
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return "", privy.ErrInvalidToken
		}
		return "", fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrUserNotFound
	}
	rules, found, err := g.store.GetGroupRules(ctx, groupID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", ErrGroupNotFound
	}
	alreadyMember, err := g.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return "", err
	}
	if alreadyMember {
		return JoinOutcomeAlreadyMember, nil
	}
	switch rules.JoinPolicy.Mode {
	case JoinModeOpen:
		if err := g.insertMember(ctx, groupID, user.ID); err != nil {
			return "", err
		}
		return JoinOutcomeJoined, nil
	case JoinModeRequest:
		if _, pending, err := g.store.GetPendingJoinRequest(ctx, groupID, user.ID); err != nil {
			return "", err
		} else if pending {
			return JoinOutcomePending, nil
		}
		if _, err := g.store.InsertJoinRequest(ctx, groupID, user.ID); err != nil {
			if _, pending, pendingErr := g.store.GetPendingJoinRequest(ctx, groupID, user.ID); pendingErr == nil && pending {
				return JoinOutcomePending, nil
			}
			return "", err
		}
		return JoinOutcomePending, nil
	default:
		return "", fmt.Errorf("invalid join mode")
	}
}

func (g *GovernanceService) insertMember(ctx context.Context, groupID, userID string) error {
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
	if err := g.store.InsertGroupMemberTx(ctx, tx, groupID, userID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit join group: %w", err)
	}
	committed = true
	return nil
}

func (g *GovernanceService) ListPendingJoinRequests(ctx context.Context, accessToken, groupID string) ([]JoinRequest, error) {
	if _, _, err := g.authenticatedGroupAdmin(ctx, accessToken, groupID); err != nil {
		return nil, err
	}
	rows, err := g.store.ListPendingJoinRequests(ctx, groupID)
	if err != nil {
		return nil, err
	}
	items := make([]JoinRequest, 0, len(rows))
	for _, row := range rows {
		items = append(items, JoinRequest{ID: row.ID, UserID: row.UserID, DisplayName: row.DisplayName, ProfilePhotoURL: row.ProfilePhotoURL, RequestedAt: row.CreatedAt})
	}
	return items, nil
}

func (g *GovernanceService) ApproveJoinRequest(ctx context.Context, accessToken, groupID, requestID string) error {
	return g.decideJoinRequest(ctx, accessToken, groupID, requestID, domain.JoinRequestApproved)
}

func (g *GovernanceService) DenyJoinRequest(ctx context.Context, accessToken, groupID, requestID string) error {
	return g.decideJoinRequest(ctx, accessToken, groupID, requestID, domain.JoinRequestDenied)
}

func (g *GovernanceService) decideJoinRequest(ctx context.Context, accessToken, groupID, requestID string, status domain.JoinRequestStatus) error {
	if _, _, err := g.authenticatedGroupAdmin(ctx, accessToken, groupID); err != nil {
		return err
	}
	row, found, err := g.store.GetJoinRequestByID(ctx, requestID)
	if err != nil {
		return err
	}
	if !found || row.GroupID != groupID || row.Status != domain.JoinRequestPending {
		return ErrJoinRequestNotFound
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
	if err := g.store.UpdateJoinRequestStatusTx(ctx, tx, requestID, status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrJoinRequestNotFound
		}
		return err
	}
	if status == domain.JoinRequestApproved {
		member, err := g.store.IsGroupMember(ctx, groupID, row.UserID)
		if err != nil {
			return err
		}
		if !member {
			if err := g.store.InsertGroupMemberTx(ctx, tx, groupID, row.UserID); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit join request decision: %w", err)
	}
	committed = true
	return nil
}

// LeaveGroupRequest is input for POST /v1/groups/{id}/leave.
type LeaveGroupRequest struct {
	AccessToken   string
	GroupID       string
	WithdrawStake bool
}

// LeaveGroup removes a member when leave policy preconditions pass.
// Positions rows are kept for deposit history; non-members are excluded from boards via group_members.
// Creators with other members must transfer ownership before leaving.
// When WithdrawStake is true, full stake is withdrawn to platform balance before membership removal.
func (g *GovernanceService) LeaveGroup(ctx context.Context, req LeaveGroupRequest) error {
	if req.GroupID == "" {
		return fmt.Errorf("group id is required")
	}
	accessToken := req.AccessToken
	groupID := req.GroupID
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
	group, groupFound, err := g.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return err
	}
	if !groupFound {
		return ErrGroupNotFound
	}
	member, err := g.store.IsGroupMember(ctx, groupID, user.ID)
	if err != nil {
		return err
	}
	if !member {
		return ErrNotGroupMemberForLeave
	}
	if req.WithdrawStake {
		position, hasPosition, err := g.store.GetPosition(ctx, user.ID, groupID)
		if err != nil {
			return err
		}
		if hasPosition && position.ShareUnits > 0 {
			if g.redeem == nil {
				return fmt.Errorf("redeem service is required for withdraw stake")
			}
			if _, err := g.redeem.WithdrawToBalance(ctx, WithdrawToBalanceRequest{
				AccessToken: accessToken,
				GroupID:     groupID,
			}); err != nil {
				return err
			}
		}
	}
	logGovernanceLeaveGroupStart(user.ID, groupID)
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
	lockedGroup, lockedFound, err := g.store.GetGroupByIDForUpdateTx(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if !lockedFound {
		return ErrGroupNotFound
	}
	memberIDs, err := g.store.ListGroupMemberIDsForUpdateTx(ctx, tx, groupID)
	if err != nil {
		return err
	}
	isMember := false
	for _, memberID := range memberIDs {
		if memberID == user.ID {
			isMember = true
			break
		}
	}
	if !isMember {
		return ErrNotGroupMemberForLeave
	}
	if err := g.validateLeavePolicyTx(ctx, tx, groupID, user.ID, lockedGroup.CreatorUserID, memberIDs); err != nil {
		return err
	}
	if err := g.store.DeleteGroupMemberTx(ctx, tx, groupID, user.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit leave group: %w", err)
	}
	committed = true
	logGovernanceLeaveGroupSuccess(user.ID, groupID, group.CreatorUserID == user.ID)
	return nil
}

func (g *GovernanceService) validateLeavePolicyTx(ctx context.Context, tx *sql.Tx, groupID, userID, creatorUserID string, memberIDs []string) error {
	if userID == creatorUserID && len(memberIDs) > 1 {
		return &LeaveGroupError{Reason: LeaveBlockCreatorMustTransfer}
	}
	position, hasPosition, err := g.store.GetPositionForUpdateTx(ctx, tx, userID, groupID)
	if err != nil {
		return err
	}
	if hasPosition && position.ShareUnits > 0 {
		return &LeaveGroupError{Reason: LeaveBlockShareUnits}
	}
	activeRedeem, err := g.store.HasActiveRedeemJobForUserTx(ctx, tx, userID, groupID)
	if err != nil {
		return err
	}
	if activeRedeem {
		return &LeaveGroupError{Reason: LeaveBlockPendingRedeem}
	}
	if len(memberIDs) == 1 {
		treasuryUSDC, err := g.groupTreasuryUSDCForLeaveTx(ctx, tx, groupID)
		if err != nil {
			return err
		}
		totalShares, err := g.store.SumShareUnitsByGroupTx(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if treasuryUSDC > 0 || totalShares > 0 {
			return &LeaveGroupError{Reason: LeaveBlockLastMemberTreasury}
		}
	}
	rules, found, err := g.store.GetGroupRulesTx(ctx, tx, groupID)
	if err != nil {
		return err
	}
	if !found {
		return ErrGroupNotFound
	}
	soleVote, err := g.hasSoleRemainingVoteTx(ctx, tx, groupID, userID, rules, memberIDs)
	if err != nil {
		return err
	}
	if soleVote {
		return &LeaveGroupError{Reason: LeaveBlockSoleRemainingVote}
	}
	return nil
}

func (g *GovernanceService) groupTreasuryUSDCForLeaveTx(ctx context.Context, tx *sql.Tx, groupID string) (int64, error) {
	netUsdcIn, err := g.store.NetUSDCInByGroupTx(ctx, tx, groupID)
	if err != nil {
		return 0, err
	}
	treasury, found, err := g.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		balance, err := g.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err == nil && balance > 0 {
			return balance, nil
		}
	}
	return netUsdcIn, nil
}

func (g *GovernanceService) hasSoleRemainingVoteTx(ctx context.Context, tx *sql.Tx, groupID, userID string, rules GroupRules, memberIDs []string) (bool, error) {
	proposals, err := g.store.ListProposalsByGroupIDTx(ctx, tx, groupID, []domain.ProposalStatus{ProposalOpen})
	if err != nil {
		return false, err
	}
	if len(proposals) == 0 {
		return false, nil
	}
	voterSet, voterIDs, err := g.resolveVoterSetTx(ctx, tx, groupID, rules, memberIDs)
	if err != nil {
		return false, err
	}
	if !domain.MemberMayVote(voterSet, userID, voterIDs) {
		return false, nil
	}
	for _, proposal := range proposals {
		if proposal.Status == ProposalOpen && g.now().UTC().Unix() >= proposal.ExpiresAt.Unix() {
			continue
		}
		votes, err := g.store.ListVotesForProposalTx(ctx, tx, proposal.ID)
		if err != nil {
			return false, err
		}
		cast := make(map[string]domain.VoteChoice, len(votes))
		for _, vote := range votes {
			cast[vote.VoterID] = vote.Choice
		}
		uncast := 0
		userUncast := false
		for _, voterID := range voterIDs {
			if _, voted := cast[voterID]; voted {
				continue
			}
			uncast++
			if voterID == userID {
				userUncast = true
			}
		}
		if userUncast && uncast == 1 {
			return true, nil
		}
	}
	return false, nil
}

func (g *GovernanceService) authenticatedGroupAdmin(ctx context.Context, accessToken, groupID string) (postgres.User, postgres.Group, error) {
	identity, err := g.privy.VerifySession(ctx, privy.AccessToken(accessToken))
	if err != nil {
		if errors.Is(err, privy.ErrInvalidToken) {
			return postgres.User{}, postgres.Group{}, privy.ErrInvalidToken
		}
		return postgres.User{}, postgres.Group{}, fmt.Errorf("verify session: %w", err)
	}
	user, found, err := g.store.GetUserByPrivyUserID(ctx, identity.PrivyUserID)
	if err != nil {
		return postgres.User{}, postgres.Group{}, err
	}
	if !found {
		return postgres.User{}, postgres.Group{}, ErrUserNotFound
	}
	group, groupFound, err := g.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return postgres.User{}, postgres.Group{}, err
	}
	if !groupFound {
		return postgres.User{}, postgres.Group{}, ErrGroupNotFound
	}
	if group.CreatorUserID != user.ID {
		return postgres.User{}, postgres.Group{}, ErrNotGroupAdmin
	}
	return user, group, nil
}

func validateCreateRules(rules GroupRules) error {
	if rules.VoteExpirySeconds <= 0 {
		return fmt.Errorf("%w: vote expiry must be positive", ErrInvalidGroupRules)
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
	case JoinModeOpen, JoinModeRequest:
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

// CreateProposal inserts an open buy or sell proposal when the quote is routable.
func (g *GovernanceService) CreateProposal(ctx context.Context, in CreateProposalInput) (Proposal, error) {
	kind := in.Kind
	if kind == "" {
		kind = domain.ProposalKindBuy
	}
	logGovernanceCreateProposalStart(in.GroupID, in.ProposerID, in.Symbol, in.UsdcMicros)

	if in.GroupID == "" || in.ProposerID == "" {
		logGovernanceBranchWarn("governance create proposal rejected", "missing ids")
		return Proposal{}, fmt.Errorf("group_id and proposer_id are required")
	}
	if in.Symbol == "" && !domain.IsAgentGovernanceKind(kind) {
		logGovernanceBranchWarn("governance create proposal rejected", "symbol required", "group_id", in.GroupID)
		return Proposal{}, fmt.Errorf("symbol is required")
	}

	member, err := g.store.IsGroupMember(ctx, in.GroupID, in.ProposerID)
	if err != nil {
		logGovernanceBranchError("governance create proposal membership check failed", err, "group_id", in.GroupID, "proposer_id", in.ProposerID)
		return Proposal{}, err
	}
	if !member {
		logGovernanceBranchWarn("governance create proposal rejected", "not group member", "group_id", in.GroupID, "proposer_id", in.ProposerID)
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
		logGovernanceBranchWarn("governance create proposal rejected", "not eligible proposer", "group_id", in.GroupID, "proposer_id", in.ProposerID)
		return Proposal{}, ErrNotEligibleProposer
	}

	switch kind {
	case domain.ProposalKindBuy:
		if in.UsdcMicros <= 0 || in.TokenAmount != 0 {
			logGovernanceBranchWarn("governance create proposal rejected", "usdc not positive", "group_id", in.GroupID)
			return Proposal{}, fmt.Errorf("usdc must be positive")
		}
		if g.buy == nil {
			logGovernanceBranchWarn("governance create proposal rejected", "buy service missing", "group_id", in.GroupID)
			return Proposal{}, fmt.Errorf("buy service is required")
		}
		treasuryTotal, err := g.proposalTreasuryTotalMicros(ctx, in.GroupID)
		if err != nil {
			logGovernanceBranchError("governance create proposal treasury balance failed", err, "group_id", in.GroupID, "proposer_id", in.ProposerID)
			return Proposal{}, err
		}
		if in.UsdcMicros > treasuryTotal {
			logGovernanceBranchWarn("governance create proposal rejected", "exceeds treasury total", "group_id", in.GroupID, "proposer_id", in.ProposerID, "usdc_micros", in.UsdcMicros, "treasury_total_micros", treasuryTotal)
			return Proposal{}, ErrExceedsTreasuryUSDC
		}
		_, err = g.buy.StartBuy(ctx, StartBuyRequest{
			GroupID:    in.GroupID,
			UserID:     in.ProposerID,
			Symbol:     in.Symbol,
			USDCAmount: in.UsdcMicros,
		})
		if err != nil {
			if errors.Is(err, ErrQuoteNotRoutable) {
				logGovernanceBranchWarn("governance create proposal rejected", "quote not routable", "group_id", in.GroupID, "proposer_id", in.ProposerID, "symbol", in.Symbol)
				return Proposal{}, ErrQuoteNotRoutable
			}
			logGovernanceBranchError("governance create proposal start buy failed", err, "group_id", in.GroupID, "proposer_id", in.ProposerID)
			return Proposal{}, err
		}
	case domain.ProposalKindSell:
		if in.TokenAmount <= 0 || in.UsdcMicros != 0 {
			return Proposal{}, fmt.Errorf("token amount must be positive")
		}
		if _, err := g.quoteSellForMember(ctx, QuoteProposalInput{
			GroupID:     in.GroupID,
			UserID:      in.ProposerID,
			Symbol:      in.Symbol,
			Kind:        domain.ProposalKindSell,
			TokenAmount: in.TokenAmount,
		}); err != nil {
			return Proposal{}, err
		}
	case domain.ProposalKindAddAgent, domain.ProposalKindPauseAgent, domain.ProposalKindResumeAgent, domain.ProposalKindRevokeAgent:
		if err := g.validateAgentProposal(ctx, CreateAgentProposalInput{
			GroupID:              in.GroupID,
			ProposerID:           in.ProposerID,
			Kind:                 kind,
			AgentDisplayName:     in.AgentDisplayName,
			AllocationUsdcMicros: in.AllocationUsdcMicros,
		}); err != nil {
			return Proposal{}, err
		}
	default:
		return Proposal{}, fmt.Errorf("invalid proposal kind")
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

	row, err := g.store.InsertProposalTx(ctx, tx, postgres.InsertProposalParams{
		GroupID:              in.GroupID,
		ProposerID:           in.ProposerID,
		Symbol:               in.Symbol,
		Kind:                 kind,
		UsdcMicros:           in.UsdcMicros,
		TokenAmount:          in.TokenAmount,
		AgentDisplayName:     in.AgentDisplayName,
		AllocationUsdcMicros: in.AllocationUsdcMicros,
		ExpiresAt:            expiresAt,
	})
	if err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		logGovernanceBranchError("governance create proposal commit failed", err, "group_id", in.GroupID, "proposer_id", in.ProposerID)
		return Proposal{}, fmt.Errorf("commit create proposal: %w", err)
	}
	committed = true

	logGovernanceCreateProposalSuccess(row.ID, in.GroupID, in.ProposerID)
	return proposalFromRow(row), nil
}

func (g *GovernanceService) proposalTreasuryTotalMicros(ctx context.Context, groupID string) (int64, error) {
	if g.home != nil {
		return g.home.GroupTreasuryTotalMicros(ctx, groupID)
	}
	return g.groupTreasuryUSDC(ctx, groupID)
}

func (g *GovernanceService) groupTreasuryUSDC(ctx context.Context, groupID string) (int64, error) {
	positions, err := g.store.ListPositionsByGroup(ctx, groupID)
	if err != nil {
		return 0, err
	}
	var netUsdcIn int64
	for _, position := range positions {
		netUsdcIn += position.AmountDeposited - position.AmountWithdrawn
	}

	treasury, found, err := g.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if found {
		balance, err := g.privy.TreasuryUSDCBalance(ctx, treasury.SolanaAddress)
		if err != nil {
			return 0, fmt.Errorf("treasury usdc balance: %w", err)
		}
		return balance, nil
	}
	return netUsdcIn, nil
}

// CastVote persists a ballot and tallies to passed, failed, or expired (M4-T14).
func (g *GovernanceService) CastVote(ctx context.Context, in CastVoteInput) (Proposal, error) {
	logGovernanceCastVoteStart(in.ProposalID, in.VoterID, string(in.Choice))

	if in.ProposalID == "" || in.VoterID == "" {
		logGovernanceBranchWarn("governance cast vote rejected", "missing ids")
		return Proposal{}, fmt.Errorf("proposal_id and voter_id are required")
	}
	switch in.Choice {
	case domain.VoteYes, domain.VoteNo:
	default:
		logGovernanceBranchWarn("governance cast vote rejected", "invalid choice", "proposal_id", in.ProposalID)
		return Proposal{}, fmt.Errorf("invalid vote choice")
	}

	row, found, err := g.store.GetProposalByID(ctx, in.ProposalID)
	if err != nil {
		logGovernanceBranchError("governance cast vote lookup proposal failed", err, "proposal_id", in.ProposalID)
		return Proposal{}, err
	}
	if !found {
		logGovernanceBranchWarn("governance cast vote rejected", "proposal not found", "proposal_id", in.ProposalID)
		return Proposal{}, ErrProposalNotFound
	}
	proposal := proposalFromRow(row)

	_, foundVote, err := g.store.GetVoteByProposalAndVoter(ctx, in.ProposalID, in.VoterID)
	if err != nil {
		logGovernanceBranchError("governance cast vote lookup existing failed", err, "proposal_id", in.ProposalID, "voter_id", in.VoterID)
		return Proposal{}, err
	}
	if foundVote {
		logGovernanceCastVoteIdempotent(in.ProposalID, in.VoterID)
		return proposal, nil
	}

	if proposal.Status == ProposalOpen && g.now().UTC().Unix() >= proposal.ExpiresAt {
		slog.Info("governance cast vote finalize expired", "proposal_id", in.ProposalID)
		proposal, err = g.FinalizeExpiredProposal(ctx, in.ProposalID)
		if err != nil {
			logGovernanceBranchError("governance cast vote finalize expired failed", err, "proposal_id", in.ProposalID)
			return Proposal{}, err
		}
	}
	if proposal.Status != ProposalOpen {
		logGovernanceBranchWarn("governance cast vote rejected", "proposal not open", "proposal_id", in.ProposalID, "status", proposal.Status)
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
		logGovernanceBranchWarn("governance cast vote rejected", "not eligible voter", "proposal_id", in.ProposalID, "voter_id", in.VoterID)
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

	prevStatus := proposal.Status
	updated, err := g.tallyAndPersistTx(ctx, tx, proposal, rules, voterIDs)
	if err != nil {
		logGovernanceBranchError("governance cast vote tally failed", err, "proposal_id", in.ProposalID)
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		logGovernanceBranchError("governance cast vote commit failed", err, "proposal_id", in.ProposalID)
		return Proposal{}, fmt.Errorf("commit cast vote: %w", err)
	}
	committed = true
	if updated.Status != prevStatus {
		logGovernanceProposalStatusTransition(in.ProposalID, prevStatus, updated.Status)
	}
	slog.Info("governance cast vote success", "proposal_id", in.ProposalID, "voter_id", in.VoterID, "status", updated.Status)
	return updated, nil
}

// FinalizeExpiredProposal marks an open past-deadline proposal expired with no swap (M4-T17).
func (g *GovernanceService) FinalizeExpiredProposal(ctx context.Context, proposalID string) (Proposal, error) {
	slog.Info("governance finalize expired start", "proposal_id", proposalID)
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
		logGovernanceBranchError("governance finalize expired commit failed", err, "proposal_id", proposalID)
		return Proposal{}, fmt.Errorf("commit finalize expired proposal: %w", err)
	}
	committed = true
	if proposal.Status == ProposalExpired {
		logGovernanceProposalStatusTransition(proposalID, ProposalOpen, ProposalExpired)
	}
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

func (g *GovernanceService) resolveVoterSetTx(ctx context.Context, tx *sql.Tx, groupID string, rules GroupRules, memberIDs []string) (VoterSet, []string, error) {
	vs := rules.VoterSet
	switch vs.Mode {
	case VoterSetAllMembers:
		return vs, memberIDs, nil
	case VoterSetNamed:
		ids, err := g.store.ListGroupVoterIDsTx(ctx, tx, groupID)
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
		slog.Info("governance proposal expired", "proposal_id", proposalID)
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

	row, found, err := g.store.GetProposalByIDTx(ctx, tx, proposal.ID)
	if err != nil {
		return Proposal{}, err
	}
	if !found {
		return Proposal{}, ErrProposalNotFound
	}

	ok, err := g.store.UpdateProposalStatusTx(ctx, tx, proposal.ID, ProposalOpen, nextStatus)
	if err != nil {
		return Proposal{}, err
	}
	if ok {
		logGovernanceProposalStatusTransition(proposal.ID, ProposalOpen, nextStatus)
		proposal.Status = nextStatus
		if nextStatus == ProposalPassed && domain.IsAgentGovernanceKind(proposal.Kind) {
			if err := g.handleAgentProposalPassTx(ctx, tx, proposal, row); err != nil {
				return Proposal{}, err
			}
		}
	}
	return proposal, nil
}

func proposalFromRow(row postgres.ProposalRow) Proposal {
	return Proposal{
		ID:          row.ID,
		GroupID:     row.GroupID,
		ProposerID:  row.ProposerID,
		Symbol:      row.Symbol,
		Kind:        row.Kind,
		UsdcMicros:  row.UsdcMicros,
		TokenAmount: row.TokenAmount,
		Status:      row.Status,
		ExpiresAt:   row.ExpiresAt.UTC().Unix(),
	}
}
