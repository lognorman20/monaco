package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/packages/domain"
)

// #153 step 2: faker scale clubs are spectator-readable by any authed user, mutations are
// rejected, the operator is never added to faker group_members, and ghost members in a real
// club show P&L without touching real equity. Fixtures use raw SQL (independent of the seeder).

type spectatorPrivy struct {
	privy.Client
	mu               sync.Mutex
	treasuryBalances []string
	ensureTreasury   []string
}

func (s *spectatorPrivy) TreasuryUSDCBalance(ctx context.Context, address string) (int64, error) {
	s.mu.Lock()
	s.treasuryBalances = append(s.treasuryBalances, address)
	s.mu.Unlock()
	return s.Client.TreasuryUSDCBalance(ctx, address)
}

func (s *spectatorPrivy) EnsureTreasury(ctx context.Context, groupID privy.GroupID) (privy.TreasuryRef, error) {
	s.mu.Lock()
	s.ensureTreasury = append(s.ensureTreasury, string(groupID))
	s.mu.Unlock()
	return s.Client.EnsureTreasury(ctx, groupID)
}

type spectatorFixture struct {
	h               integrationHarness
	privy           *spectatorPrivy
	home            *HomeService
	governance      *GovernanceService
	operatorID      string
	operatorToken   string
	realGroupID     string
	ghostID         string
	ghostProposalID string
	fakerGroupID    string
	fakerTreasury   string
	fakerAID        string
	fakerBID        string
	fakerOpenID     string
	fakerTxID       string
}

func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %s: %v", strings.Fields(q)[0:3], err)
	}
}

func queryID(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&id); err != nil {
		t.Fatalf("query %s: %v", strings.Fields(q)[0:3], err)
	}
	return id
}

func newSpectatorFixture(t *testing.T) spectatorFixture {
	t.Helper()
	h := integrationApp(t)
	rec := &spectatorPrivy{Client: h.Privy}
	ctx := context.Background()
	db := h.DB
	sfx := h.ISO.Suffix()

	deposits := NewDepositService(h.Store, rec, h.Pyth, h.Symbols)
	home := NewHomeService(h.Store, rec, h.Pyth, deposits, h.Symbols)
	governance := NewGovernanceService(h.Store, rec)
	governance.SetBuyService(NewBuyService(h.Jupiter, h.XStocks))
	governance.SetHomeService(home)

	fx := spectatorFixture{h: h, privy: rec, home: home, governance: governance, fakerTreasury: "faker-treasury-" + sfx}

	sessions := NewSessionService(h.Store, h.Privy)
	op := openTestSession(t, h.ISO, sessions, h.Privy, "operator", "Operator")
	fx.operatorID = op.UserID
	fx.operatorToken = h.ISO.UniqueToken("operator")
	group, err := governance.CreateGroupWithRules(ctx, fx.operatorToken, testGroupName(h.ISO, "operator"), DefaultGroupRules())
	if err != nil {
		t.Fatalf("CreateGroupWithRules: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	fx.realGroupID = group.GroupID
	// Operator holds 10 USDC at NAV 1.0 in the real treasury.
	privy.SetTreasuryUSDCBalance(h.Privy, group.TreasuryAddress, 10_000_000)
	execSQL(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 10000000, 10000000)`, fx.operatorID, fx.realGroupID)

	newFaker := func(label, name string) string {
		id := queryID(t, db, `INSERT INTO users (privy_user_id, display_name, is_faker) VALUES ($1, $2, true) RETURNING id`, "faker:user:test-"+sfx+"-"+label, name)
		h.ISO.TrackUser(id)
		return id
	}

	// Ghost member in the real club: 2 USDC in, 2.4 shares (+20% seeded P&L), passed proposal w/o swap.
	fx.ghostID = newFaker("ghost", "Maya Chen")
	execSQL(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, fx.realGroupID, fx.ghostID)
	execSQL(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 2400000, 2000000)`, fx.ghostID, fx.realGroupID)
	fx.ghostProposalID = queryID(t, db, `INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at) VALUES ($1, $2, 'AAPLx', 1000000, 'open', now() + interval '1 day') RETURNING id`, fx.realGroupID, fx.ghostID)
	execSQL(t, db, `INSERT INTO votes (proposal_id, voter_id, choice) VALUES ($1, $2, 'yes')`, fx.ghostProposalID, fx.ghostID)

	// Faker scale club: dummy treasury, two members, confirmed AAPLx buy, open proposal.
	fx.fakerAID = newFaker("a", "Rowan Ellis")
	fx.fakerBID = newFaker("b", "Tess Morgan")
	fx.fakerGroupID = queryID(t, db, `INSERT INTO groups (name, creator_user_id, is_faker, faker_key) VALUES ($1, $2, true, $3) RETURNING id`, "Scale "+sfx, fx.fakerAID, "test:"+sfx)
	h.ISO.TrackGroup(fx.fakerGroupID)
	execSQL(t, db, `INSERT INTO treasuries (group_id, privy_wallet_id, solana_address) VALUES ($1, $2, $3)`, fx.fakerGroupID, "faker:treasury:"+sfx, fx.fakerTreasury)
	for _, id := range []string{fx.fakerAID, fx.fakerBID} {
		execSQL(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, fx.fakerGroupID, id)
	}
	execSQL(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 6000000, 6000000)`, fx.fakerAID, fx.fakerGroupID)
	execSQL(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, 3600000, 4000000)`, fx.fakerBID, fx.fakerGroupID)
	execSQL(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature) VALUES ($1, $2, 6000000, 'faker-wallet-a', 'confirmed', $3)`, fx.fakerAID, fx.fakerGroupID, "faker-sig-a-"+sfx)
	passedID := queryID(t, db, `INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at) VALUES ($1, $2, 'AAPLx', 4000000, 'passed', now() - interval '1 day') RETURNING id`, fx.fakerGroupID, fx.fakerAID)
	fx.fakerTxID = queryID(t, db, `INSERT INTO transactions (group_id, proposal_id, amount, action, input_mint, output_mint, status, tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at)
VALUES ($1, $2, 4000000, 'buy', $3, $4, 'confirmed', $5, $6, 4000000, 20000, now()) RETURNING id`,
		fx.fakerGroupID, passedID, jupiter.USDCMint, jupiter.AAPLxMint, "faker-sig-buy-"+sfx, "faker-req-"+sfx)
	fx.fakerOpenID = queryID(t, db, `INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at) VALUES ($1, $2, 'TSLAx', 1000000, 'open', now() + interval '1 day') RETURNING id`, fx.fakerGroupID, fx.fakerBID)
	return fx
}

func (fx spectatorFixture) assertNoFakerPrivy(t *testing.T) {
	t.Helper()
	for _, addr := range fx.privy.treasuryBalances {
		if addr == fx.fakerTreasury {
			t.Errorf("TreasuryUSDCBalance called for faker treasury")
		}
	}
	for _, id := range fx.privy.ensureTreasury {
		if id == fx.fakerGroupID {
			t.Errorf("EnsureTreasury called for faker group")
		}
	}
}

func TestFakerSpectator_nonMemberReadsScaleClubWithoutPrivy(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()

	view, err := fx.home.GetGroupView(ctx, fx.operatorToken, fx.fakerGroupID)
	if err != nil {
		t.Fatalf("GetGroupView(faker) as spectator: %v", err)
	}
	if view.TreasuryAddress != "" {
		t.Errorf("TreasuryAddress = %q, want empty for faker club", view.TreasuryAddress)
	}
	if len(view.Members) != 2 {
		t.Errorf("members = %d, want 2", len(view.Members))
	}
	if view.You.ShareUnits != "0" || view.You.EquityUsd != "0.00" {
		t.Errorf("you slice = %+v, want zeros", view.You)
	}
	// Ledger NAV: 10 in − 4 spent = 6 USDC + 4 USDC of AAPLx at cost (no Pyth mark registered).
	if view.PotTotalUsd != "10.00" {
		t.Errorf("PotTotalUsd = %s, want 10.00", view.PotTotalUsd)
	}

	activity, err := fx.home.ListGroupActivity(ctx, fx.operatorToken, fx.fakerGroupID)
	if err != nil {
		t.Fatalf("ListGroupActivity(faker): %v", err)
	}
	if len(activity) < 2 {
		t.Errorf("activity rows = %d, want deposit + buy", len(activity))
	}

	open, err := fx.governance.ListGroupProposals(ctx, fx.operatorToken, fx.fakerGroupID, "open")
	if err != nil || len(open) != 1 {
		t.Fatalf("ListGroupProposals(open) = %d rows, err %v", len(open), err)
	}
	detail, err := fx.governance.GetProposalDetail(ctx, fx.operatorToken, fx.fakerOpenID)
	if err != nil {
		t.Fatalf("GetProposalDetail(faker): %v", err)
	}
	if detail.CanVote {
		t.Error("spectator CanVote = true on faker proposal")
	}


	member, err := fx.h.Store.IsGroupMember(ctx, fx.fakerGroupID, fx.operatorID)
	if err != nil || member {
		t.Fatalf("operator member of faker group = %v (err %v), want false", member, err)
	}
	fx.assertNoFakerPrivy(t)
}

func TestFakerSpectator_mutationsRejectedOnScaleClub(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()

	if _, err := fx.governance.JoinGroup(ctx, fx.operatorToken, fx.fakerGroupID); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("JoinGroup err = %v, want ErrFakerGroupReadOnly", err)
	}
	if err := fx.governance.LeaveGroup(ctx, LeaveGroupRequest{AccessToken: fx.operatorToken, GroupID: fx.fakerGroupID, WithdrawStake: true}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("LeaveGroup err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.home.deposits.CreateDeposit(ctx, fx.operatorToken, fx.fakerGroupID, 1_000_000); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("CreateDeposit err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.governance.CreateProposal(ctx, CreateProposalInput{GroupID: fx.fakerGroupID, ProposerID: fx.operatorID, Symbol: "AAPLx", UsdcMicros: 1_000_000}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("CreateProposal err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.governance.CastVote(ctx, CastVoteInput{ProposalID: fx.fakerOpenID, VoterID: fx.operatorID, Choice: domain.VoteYes}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("CastVote(faker club) err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := fx.h.Swap.RetryFailedSwap(ctx, RetryFailedSwapRequest{TransactionID: fx.fakerTxID, UserID: fx.operatorID}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("RetryFailedSwap err = %v, want ErrFakerGroupReadOnly", err)
	}

	member, err := fx.h.Store.IsGroupMember(ctx, fx.fakerGroupID, fx.operatorID)
	if err != nil || member {
		t.Fatalf("operator became faker group member (err %v)", err)
	}
	fx.assertNoFakerPrivy(t)
}

func TestFakerSpectator_homeUnionsScaleClubsWithoutMembership(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()

	home, err := fx.home.GetHome(ctx, fx.operatorToken)
	if err != nil {
		t.Fatalf("GetHome: %v", err)
	}
	foundGroup := false
	for _, row := range home.Groups {
		if row.GroupID == fx.fakerGroupID {
			foundGroup = true
			if row.IsJoined {
				t.Error("faker club IsJoined = true for operator")
			}
		}
	}
	if !foundGroup {
		t.Error("GET /v1/home groups missing faker scale club")
	}
	people := map[string]bool{}
	for _, row := range home.People {
		people[row.UserID] = true
	}
	for _, id := range []string{fx.fakerAID, fx.fakerBID, fx.ghostID, fx.operatorID} {
		if !people[id] {
			t.Errorf("home people board missing %s", id)
		}
	}

	dash, err := fx.home.GetHomeDashboard(ctx, fx.operatorToken, HomeLeaderboardRangeALL)
	if err != nil {
		t.Fatalf("GetHomeDashboard: %v", err)
	}
	for _, g := range dash.MyGroups {
		if g.GroupID == fx.fakerGroupID {
			t.Error("dashboard MyGroups includes faker club")
		}
	}
	for _, m := range dash.MissedProposals {
		if m.ProposalID == fx.ghostProposalID || m.GroupID == fx.fakerGroupID {
			t.Errorf("missed proposals include faker proposal %+v", m)
		}
	}
	leader := map[string]bool{}
	for _, row := range dash.Leaderboard.People {
		leader[row.UserID] = true
	}
	if !leader[fx.fakerAID] || !leader[fx.fakerBID] {
		t.Error("dashboard leaderboard missing faker scale club people")
	}

	fx.assertNoFakerPrivy(t)
}

func TestFakerSpectator_ghostsShowPnLWithoutDilutingOperator(t *testing.T) {
	fx := newSpectatorFixture(t)
	ctx := context.Background()

	view, err := fx.home.GetGroupView(ctx, fx.operatorToken, fx.realGroupID)
	if err != nil {
		t.Fatalf("GetGroupView(real): %v", err)
	}
	if view.PotTotalUsd != "10.00" {
		t.Errorf("real pot = %s, want 10.00 (ghost not in pot)", view.PotTotalUsd)
	}
	if view.You.EquityUsd != "10.00" || view.You.SlicePercent != "1" {
		t.Errorf("operator slice = %+v, want full 10.00 equity", view.You)
	}
	var ghost *GroupViewMemberRow
	for i := range view.Members {
		if view.Members[i].UserID == fx.ghostID {
			ghost = &view.Members[i]
		}
	}
	if ghost == nil {
		t.Fatal("ghost missing from member board")
	}
	if ghost.DollarPnL != "+0.40" {
		t.Errorf("ghost DollarPnL = %s, want +0.40", ghost.DollarPnL)
	}

	// Ghost proposal: display-only. Operator vote is rejected and eligibility shows all members.
	if _, err := fx.governance.CastVote(ctx, CastVoteInput{ProposalID: fx.ghostProposalID, VoterID: fx.operatorID, Choice: domain.VoteYes}); !errors.Is(err, ErrFakerGroupReadOnly) {
		t.Errorf("CastVote(ghost proposal) err = %v, want ErrFakerGroupReadOnly", err)
	}
	detail, err := fx.governance.GetProposalDetail(ctx, fx.operatorToken, fx.ghostProposalID)
	if err != nil {
		t.Fatalf("GetProposalDetail(ghost): %v", err)
	}
	if detail.CanVote || detail.VoteSummary.EligibleCount != 2 {
		t.Errorf("ghost proposal detail canVote=%v eligible=%d, want false/2", detail.CanVote, detail.VoteSummary.EligibleCount)
	}

	// Live voter set for real proposals is the operator alone.
	rules, _, err := fx.h.Store.GetGroupRules(ctx, fx.realGroupID)
	if err != nil {
		t.Fatalf("GetGroupRules: %v", err)
	}
	_, voters, err := fx.governance.resolveVoterSet(ctx, fx.realGroupID, rules)
	if err != nil {
		t.Fatalf("resolveVoterSet: %v", err)
	}
	if len(voters) != 1 || voters[0] != fx.operatorID {
		t.Errorf("live voters = %v, want operator only", voters)
	}
}
