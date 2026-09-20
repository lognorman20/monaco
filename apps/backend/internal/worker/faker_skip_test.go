package worker

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/auth"
	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// #153: faker rows must never reach Privy, Solana RPC, or Jupiter. These tests seed faker rows
// directly (independent of the seeder) and run the always-on workers against them.

type recordingPrivy struct {
	wallets.Client
	mu              sync.Mutex
	memberBalance   []string
	treasuryBalance []string
	ensureTreasury  []string
	sweeps          []wallets.SweepRequest
}

func (r *recordingPrivy) MemberUSDCBalance(ctx context.Context, address string) (int64, error) {
	r.mu.Lock()
	r.memberBalance = append(r.memberBalance, address)
	r.mu.Unlock()
	return r.Client.MemberUSDCBalance(ctx, address)
}

func (r *recordingPrivy) TreasuryUSDCBalance(ctx context.Context, address string) (int64, error) {
	r.mu.Lock()
	r.treasuryBalance = append(r.treasuryBalance, address)
	r.mu.Unlock()
	return r.Client.TreasuryUSDCBalance(ctx, address)
}

func (r *recordingPrivy) EnsureTreasury(ctx context.Context, groupID wallets.GroupID) (wallets.TreasuryRef, error) {
	r.mu.Lock()
	r.ensureTreasury = append(r.ensureTreasury, string(groupID))
	r.mu.Unlock()
	return r.Client.EnsureTreasury(ctx, groupID)
}

func (r *recordingPrivy) SubmitSweep(ctx context.Context, req wallets.SweepRequest) (wallets.SweepResult, error) {
	r.mu.Lock()
	r.sweeps = append(r.sweeps, req)
	r.mu.Unlock()
	return r.Client.SubmitSweep(ctx, req)
}

type recordingRPC struct {
	inner Confirmer
	mu    sync.Mutex
	sigs  []string
}

func (r *recordingRPC) IsConfirmed(ctx context.Context, sig string) (bool, error) {
	r.mu.Lock()
	r.sigs = append(r.sigs, sig)
	r.mu.Unlock()
	return r.inner.IsConfirmed(ctx, sig)
}

type recordingJupiter struct {
	dex.Client
	mu    sync.Mutex
	calls []string
}

func (r *recordingJupiter) record(op, token string) {
	r.mu.Lock()
	r.calls = append(r.calls, op+":"+token)
	r.mu.Unlock()
}

func (r *recordingJupiter) QuoteBuy(ctx context.Context, tokenOut string, usdcIn *big.Int) (dex.Quote, error) {
	r.record("quote_buy", tokenOut)
	return r.Client.QuoteBuy(ctx, tokenOut, usdcIn)
}

func (r *recordingJupiter) QuoteSell(ctx context.Context, tokenIn string, amountIn *big.Int) (dex.Quote, error) {
	r.record("quote_sell", tokenIn)
	return r.Client.QuoteSell(ctx, tokenIn, amountIn)
}

func (r *recordingJupiter) BuildSwap(ctx context.Context, q dex.Quote, sender, recipient string) (dex.SwapCall, error) {
	r.record("build_swap", q.TokenOut)
	return r.Client.BuildSwap(ctx, q, sender, recipient)
}

// fakerFixture is a hand-built faker world: one real operator club with a ghost member and one
// wholly fake scale club with a dummy treasury.
type fakerFixture struct {
	operatorID       string
	operatorToken    string
	realGroupID      string
	realTreasury     string
	ghostID          string
	ghostAddress     string
	fakerGroupID     string
	fakerTreasury    string
	fakerMemberID    string
	fakerAddress     string
	fakerBroadcast   string
	ghostProposalID  string
	fakerProposalID  string
	ghostDepositID   string
	fakerDepositIDs  []string
	ghostShareUnits  int64
	fakerGroupShares int64
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.SplitN(strings.TrimSpace(query), "\n", 2)[0], err)
	}
}

func mustQueryID(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&id); err != nil {
		t.Fatalf("query %q: %v", strings.SplitN(strings.TrimSpace(query), "\n", 2)[0], err)
	}
	return id
}

func seedFakerFixture(t *testing.T, testApp *workerTestApp, privyClient wallets.Client) fakerFixture {
	t.Helper()
	ctx := context.Background()
	db := testApp.DB
	sfx := testApp.ISO.Suffix()
	fx := fakerFixture{
		ghostAddress:     "faker-wallet-ghost-" + sfx,
		fakerAddress:     "faker-wallet-member-" + sfx,
		fakerTreasury:    "faker-treasury-" + sfx,
		fakerBroadcast:   "faker-sig-broadcast-" + sfx,
		ghostShareUnits:  7_000_000,
		fakerGroupShares: 9_000_000,
	}

	// Real operator club through the normal service path (fake Privy treasury).
	token := auth.AccessToken(testApp.ISO.UniqueToken("operator"))
	auth.RegisterToken(testApp.Auth, token, auth.Identity{DynamicUserID: testApp.ISO.UniqueDynamicID("operator"), DisplayName: "Operator"})
	session, err := app.NewSessionService(testApp.Store, testApp.Auth, privyClient).OpenSession(ctx, string(token))
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	testApp.ISO.TrackUser(session.UserID)
	fx.operatorID = session.UserID
	fx.operatorToken = string(token)
	governance := app.NewGovernanceService(testApp.Store, testApp.Auth, privyClient)
	group, err := governance.CreateGroupWithRules(ctx, string(token), "Operator Club "+sfx, app.DefaultGroupRules())
	if err != nil {
		t.Fatalf("CreateGroupWithRules: %v", err)
	}
	testApp.ISO.TrackGroup(group.GroupID)
	fx.realGroupID = group.GroupID
	fx.realTreasury = group.TreasuryAddress

	// Faker users.
	fx.ghostID = mustQueryID(t, db, `INSERT INTO users (dynamic_user_id, display_name, is_faker) VALUES ($1, 'Maya Ghost', true) RETURNING id`, "faker:user:test-"+sfx+"-ghost")
	testApp.ISO.TrackUser(fx.ghostID)
	fx.fakerMemberID = mustQueryID(t, db, `INSERT INTO users (dynamic_user_id, display_name, is_faker) VALUES ($1, 'Scale Member', true) RETURNING id`, "faker:user:test-"+sfx+"-member")
	testApp.ISO.TrackUser(fx.fakerMemberID)

	// Ghost member inside the real club: pending deposit, display position, passed proposal (no swap).
	mustExec(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, fx.realGroupID, fx.ghostID)
	fx.ghostDepositID = mustQueryID(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status) VALUES ($1, $2, 5000000, $3, 'pending') RETURNING id`, fx.ghostID, fx.realGroupID, fx.ghostAddress)
	mustExec(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, $3, 6000000)`, fx.ghostID, fx.realGroupID, fx.ghostShareUnits)
	fx.ghostProposalID = mustQueryID(t, db, `INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at) VALUES ($1, $2, 'AAPLx', 1000000, 'passed', now() - interval '1 hour') RETURNING id`, fx.realGroupID, fx.ghostID)

	// Wholly fake scale club with a dummy (non-Privy, non-FAKE*) treasury.
	fx.fakerGroupID = mustQueryID(t, db, `INSERT INTO groups (name, creator_user_id, is_faker, faker_key) VALUES ($1, $2, true, $3) RETURNING id`, "Scale Club "+sfx, fx.fakerMemberID, "test:"+sfx)
	testApp.ISO.TrackGroup(fx.fakerGroupID)
	mustExec(t, db, `INSERT INTO treasuries (group_id, wallet_id, address) VALUES ($1, $2, $3)`, fx.fakerGroupID, "faker:treasury:"+sfx, fx.fakerTreasury)
	mustExec(t, db, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, fx.fakerGroupID, fx.fakerMemberID)
	mustExec(t, db, `INSERT INTO positions (user_id, group_id, share_units, amount_deposited) VALUES ($1, $2, $3, 9000000)`, fx.fakerMemberID, fx.fakerGroupID, fx.fakerGroupShares)
	fx.fakerDepositIDs = append(fx.fakerDepositIDs,
		mustQueryID(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status) VALUES ($1, $2, 1000000, $3, 'pending') RETURNING id`, fx.fakerMemberID, fx.fakerGroupID, fx.fakerAddress),
		mustQueryID(t, db, `INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_hash) VALUES ($1, $2, 2000000, $3, 'pending', $4) RETURNING id`, fx.fakerMemberID, fx.fakerGroupID, fx.fakerAddress, fx.fakerBroadcast),
	)
	fx.fakerProposalID = mustQueryID(t, db, `INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at) VALUES ($1, $2, 'AAPLx', 1000000, 'passed', now() - interval '1 hour') RETURNING id`, fx.fakerGroupID, fx.fakerMemberID)
	return fx
}

func TestFakerSkip_memberWalletTriggerRejectsFakerUser(t *testing.T) {
	testApp := integrationWorkerApp(t)
	fx := seedFakerFixture(t, testApp, testApp.Privy)

	_, err := testApp.Store.InsertMemberWallet(context.Background(), fx.ghostID, "faker-wallet-id", "FAKE0000")
	if err == nil {
		t.Fatal("InsertMemberWallet for faker user succeeded; want trigger rejection")
	}
	if !strings.Contains(err.Error(), "faker user") {
		t.Fatalf("err = %v, want faker user trigger rejection", err)
	}
}

func TestFakerSkip_sweepPollerNeverTouchesFakerRows(t *testing.T) {
	testApp := integrationWorkerApp(t)
	rec := &recordingPrivy{Client: testApp.Privy}
	fx := seedFakerFixture(t, testApp, testApp.Privy)
	ctx := context.Background()

	// Real treasury holds 3 USDC nobody is credited for yet; ghost shares must not absorb it.
	wallets.SetTreasuryUSDCBalance(testApp.Privy, fx.realTreasury, 3_000_000)
	// If a faker sweep ever went out, these would make it "succeed" loudly.
	wallets.SetMemberUSDCBalance(testApp.Privy, fx.ghostAddress, 5_000_000)
	wallets.SetMemberUSDCBalance(testApp.Privy, fx.fakerAddress, 5_000_000)
	rpc := &recordingRPC{inner: NewFakeConfirmer()}
	deposits := app.NewDepositService(testApp.Store, testApp.Auth, rec, chainlink.NewFakeClient(), app.NewSymbolResolver(nil))
	poller := NewSweepPoller(testApp.Store, rec, rpc, deposits, "relayer-key", NewStubClock(testApp.Now))

	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	fakerAddresses := map[string]bool{fx.ghostAddress: true, fx.fakerAddress: true, fx.fakerTreasury: true}
	for _, addr := range rec.memberBalance {
		if fakerAddresses[addr] {
			t.Errorf("MemberUSDCBalance called for faker address %s", addr)
		}
	}
	for _, addr := range rec.treasuryBalance {
		if fakerAddresses[addr] {
			t.Errorf("TreasuryUSDCBalance called for faker address %s", addr)
		}
	}
	for _, sweep := range rec.sweeps {
		if fakerAddresses[sweep.MemberAddress] || fakerAddresses[sweep.TreasuryAddress] {
			t.Errorf("SubmitSweep called with faker address: %+v", sweep)
		}
	}
	for _, sig := range rpc.sigs {
		if sig == fx.fakerBroadcast {
			t.Errorf("RPC IsConfirmed called for faker signature %s", sig)
		}
	}
	for _, groupID := range rec.ensureTreasury {
		if groupID == fx.fakerGroupID {
			t.Errorf("EnsureTreasury called for faker group")
		}
	}

	// Faker deposits stay untouched.
	for _, id := range append([]string{fx.ghostDepositID}, fx.fakerDepositIDs...) {
		row, found, err := testApp.Store.GetDepositByID(ctx, id)
		if err != nil || !found {
			t.Fatalf("GetDepositByID(%s): found=%v err=%v", id, found, err)
		}
		if row.Status != "pending" {
			t.Errorf("faker deposit %s status = %q, want pending (never processed)", id, row.Status)
		}
	}

	// Ghost pending deposit did not freeze surplus: operator (sole real holder) got the full 3 USDC.
	operatorPos, found, err := testApp.Store.GetPosition(ctx, fx.operatorID, fx.realGroupID)
	if err != nil || !found {
		t.Fatalf("operator position: found=%v err=%v", found, err)
	}
	if operatorPos.ShareUnits != 3_000_000 || operatorPos.AmountDeposited != 3_000_000 {
		t.Fatalf("operator position = %+v, want 3 USDC surplus credited 1:1", operatorPos)
	}
	ghostPos, _, err := testApp.Store.GetPosition(ctx, fx.ghostID, fx.realGroupID)
	if err != nil {
		t.Fatalf("ghost position: %v", err)
	}
	if ghostPos.ShareUnits != fx.ghostShareUnits || ghostPos.AmountDeposited != 6_000_000 {
		t.Fatalf("ghost position changed = %+v", ghostPos)
	}

	// Second tick: surplus math uses real-only shares, so nothing new is minted.
	if err := poller.Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	operatorPos, _, _ = testApp.Store.GetPosition(ctx, fx.operatorID, fx.realGroupID)
	if operatorPos.ShareUnits != 3_000_000 {
		t.Fatalf("operator share units after second tick = %d, want 3000000", operatorPos.ShareUnits)
	}
}

func TestFakerSkip_storeFiltersExcludeFakerRows(t *testing.T) {
	testApp := integrationWorkerApp(t)
	fx := seedFakerFixture(t, testApp, testApp.Privy)
	ctx := context.Background()
	store := testApp.Store

	pending, err := store.ListPendingDeposits(ctx)
	if err != nil {
		t.Fatalf("ListPendingDeposits: %v", err)
	}
	for _, row := range pending {
		if row.UserID == fx.ghostID || row.UserID == fx.fakerMemberID || row.GroupID == fx.fakerGroupID {
			t.Errorf("ListPendingDeposits returned faker row %+v", row)
		}
	}

	hasPending, err := store.HasPendingDepositsForGroup(ctx, fx.realGroupID)
	if err != nil {
		t.Fatalf("HasPendingDepositsForGroup: %v", err)
	}
	if hasPending {
		t.Error("HasPendingDepositsForGroup(real) = true; ghost pending deposit must not freeze surplus")
	}

	wallets, err := store.ListMemberWallets(ctx)
	if err != nil {
		t.Fatalf("ListMemberWallets: %v", err)
	}
	for _, w := range wallets {
		if w.UserID == fx.ghostID || w.UserID == fx.fakerMemberID {
			t.Errorf("ListMemberWallets returned faker wallet %+v", w)
		}
	}

	passed, err := store.ListPassedProposalsPendingExecute(ctx, 500)
	if err != nil {
		t.Fatalf("ListPassedProposalsPendingExecute: %v", err)
	}
	for _, p := range passed {
		if p.ID == fx.ghostProposalID || p.ID == fx.fakerProposalID {
			t.Errorf("ListPassedProposalsPendingExecute returned faker proposal %s", p.ID)
		}
	}

	realIDs, err := store.ListRealGroupIDs(ctx)
	if err != nil {
		t.Fatalf("ListRealGroupIDs: %v", err)
	}
	for _, id := range realIDs {
		if id == fx.fakerGroupID {
			t.Error("ListRealGroupIDs includes faker group")
		}
	}
	fakerIDs, err := store.ListFakerGroupIDs(ctx)
	if err != nil {
		t.Fatalf("ListFakerGroupIDs: %v", err)
	}
	if !containsID(fakerIDs, fx.fakerGroupID) || containsID(fakerIDs, fx.realGroupID) {
		t.Errorf("ListFakerGroupIDs = %v, want faker group only", fakerIDs)
	}

	// Ghost shares never enter the real pot sums; faker group sums include every member.
	realShares, err := store.SumShareUnitsByGroup(ctx, fx.realGroupID)
	if err != nil {
		t.Fatalf("SumShareUnitsByGroup(real): %v", err)
	}
	if realShares != 0 {
		t.Errorf("real group share sum = %d, want 0 (ghost excluded)", realShares)
	}
	fakerShares, err := store.SumShareUnitsByGroup(ctx, fx.fakerGroupID)
	if err != nil {
		t.Fatalf("SumShareUnitsByGroup(faker): %v", err)
	}
	if fakerShares != fx.fakerGroupShares {
		t.Errorf("faker group share sum = %d, want %d", fakerShares, fx.fakerGroupShares)
	}

	live, err := store.ListLiveGroupMemberIDs(ctx, fx.realGroupID)
	if err != nil {
		t.Fatalf("ListLiveGroupMemberIDs: %v", err)
	}
	if containsID(live, fx.ghostID) || !containsID(live, fx.operatorID) {
		t.Errorf("live voters = %v, want operator only", live)
	}

	ledger, err := store.FakerLedgerUSDC(ctx, fx.fakerGroupID)
	if err != nil {
		t.Fatalf("FakerLedgerUSDC: %v", err)
	}
	if ledger != 9_000_000 {
		t.Errorf("faker ledger usdc = %d, want 9000000", ledger)
	}
	if _, err := store.FakerLedgerUSDC(ctx, fx.realGroupID); err == nil {
		t.Error("FakerLedgerUSDC on a real group succeeded; want error")
	}
}

func TestFakerSkip_executeOnPassRefusesFakerProposals(t *testing.T) {
	testApp := integrationWorkerApp(t)
	rec := &recordingPrivy{Client: testApp.Privy}
	fx := seedFakerFixture(t, testApp, testApp.Privy)
	ctx := context.Background()

	jup := &recordingJupiter{Client: dex.NewFakeClient()}
	buy := app.NewBuyService(jup, b20.NewFakeCatalog())
	swap := app.NewSwapService(testApp.Store, buy, jup, rec, nil, app.NewSymbolResolver(nil))
	exec := app.NewExecuteOnPassService(swap, testApp.Store)

	// Poller tick: faker proposals are never listed, so nothing executes.
	NewProposalExecutePoller(testApp.Store, exec, NewStubClock(testApp.Now)).tick(ctx)

	// Direct calls are refused before any Privy/Jupiter call.
	for _, p := range []app.Proposal{
		{ID: fx.ghostProposalID, GroupID: fx.realGroupID, ProposerID: fx.ghostID, Symbol: "AAPLx", UsdcMicros: 1_000_000, Status: app.ProposalPassed},
		{ID: fx.fakerProposalID, GroupID: fx.fakerGroupID, ProposerID: fx.fakerMemberID, Symbol: "AAPLx", UsdcMicros: 1_000_000, Status: app.ProposalPassed},
	} {
		if _, err := exec.ExecuteOnPass(ctx, p); !errors.Is(err, app.ErrFakerGroupReadOnly) {
			t.Errorf("ExecuteOnPass(%s) err = %v, want ErrFakerGroupReadOnly", p.ID, err)
		}
	}
	if _, err := swap.DevExecuteBuy(ctx, app.DevExecuteBuyRequest{GroupID: fx.fakerGroupID, UserID: fx.operatorID, Symbol: "AAPLx", USDCAmount: 1_000_000}); !errors.Is(err, app.ErrFakerGroupReadOnly) {
		t.Errorf("DevExecuteBuy(faker group) err = %v, want ErrFakerGroupReadOnly", err)
	}
	if _, err := swap.SellToUSDC(ctx, app.SellToUSDCRequest{GroupID: fx.fakerGroupID, UserID: fx.operatorID, Symbol: "AAPLx", InputToken: "0xb200000000000000000000c2e324d24d7eecd1fb", Amount: 1}); !errors.Is(err, app.ErrFakerGroupReadOnly) {
		t.Errorf("SellToUSDC(faker group) err = %v, want ErrFakerGroupReadOnly", err)
	}

	for _, call := range jup.calls {
		if strings.HasSuffix(call, ":"+fx.fakerGroupID) || strings.HasSuffix(call, ":"+fx.realGroupID) {
			t.Errorf("Jupiter called for faker proposal: %s", call)
		}
	}
	for _, groupID := range rec.ensureTreasury {
		if groupID == fx.fakerGroupID || groupID == fx.realGroupID {
			t.Errorf("EnsureTreasury called for group %s while executing faker proposals", groupID)
		}
	}

	// The faker proposals stay passed with no transaction rows.
	for _, id := range []string{fx.ghostProposalID, fx.fakerProposalID} {
		n, err := testApp.Store.CountTransactionsForProposal(ctx, id)
		if err != nil {
			t.Fatalf("CountTransactionsForProposal: %v", err)
		}
		if n != 0 {
			t.Errorf("proposal %s has %d transactions, want 0", id, n)
		}
	}
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
