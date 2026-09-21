package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const (
	assetSocialTestSymbol = "AAPLx"
	// 12 AAPLx bought for $2,600, marked at $232.05.
	assetSocialTestUnits    = int64(12 * jupiter.XStockAtomicScale)
	assetSocialTestCostUsdc = int64(2_600_000_000)
	assetSocialTestMarkUsdc = int64(232_050_000)
	// The viewer put $3,000 in and the cabal spent $2,600 of it on stock, so the
	// treasury holds $400. Treasury and ledger agree: nothing is waiting to be
	// credited, which is the steady state a member hits every time they tap a stock.
	assetSocialTestDeposit  = int64(3_000_000_000)
	assetSocialTestTreasury = assetSocialTestDeposit - assetSocialTestCostUsdc
)

// assetSocialFixture is a viewer in `cabals` clubs, each holding the same stock, with
// the endpoint's Postgres statements and Solana RPC reads counted.
type assetSocialFixture struct {
	h        integrationHarness
	counting *countingDB
	store    *postgres.Store
	rpc      *countingRPC
	home     *HomeService
	token    string
	userID   string
	groupIDs []string
}

func newAssetSocialFixture(t *testing.T, cabals int) assetSocialFixture {
	t.Helper()

	h := integrationApp(t)
	ctx := context.Background()

	counting := openCountingTestDB(t)
	countingStore := postgres.NewStore(counting.DB)
	rpc := &countingRPC{Client: h.Privy}

	deposits := NewDepositService(countingStore, rpc, h.Pyth, h.Symbols)
	home := NewHomeService(countingStore, rpc, h.Pyth, deposits, h.Symbols)

	sessions := NewSessionService(h.Store, h.Privy)
	session := openTestSession(t, h.ISO, sessions, h.Privy, "social", "Social Viewer")
	token := h.ISO.UniqueToken("social")

	// One registration keyed on the empty treasury ref covers a request-wide mark
	// lookup; the per-group registrations keep a per-cabal caller working too.
	markInput := pyth.NavInput{Holdings: []pyth.MarkedHolding{{
		Symbol:   assetSocialTestSymbol,
		Mint:     jupiter.AAPLxMint,
		MarkUsdc: assetSocialTestMarkUsdc,
		Source:   pyth.MarkSourcePyth,
	}}}
	pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{}, markInput)

	groupIDs := make([]string, 0, cabals)
	for i := 0; i < cabals; i++ {
		label := fmt.Sprintf("social-%d", i)
		group, err := h.Groups.CreateGroup(ctx, token, testGroupName(h.ISO, label))
		if err != nil {
			t.Fatalf("CreateGroup(%s): %v", label, err)
		}
		h.ISO.TrackGroup(group.GroupID)
		groupIDs = append(groupIDs, group.GroupID)

		privy.SetTreasuryUSDCBalance(h.Privy, group.TreasuryAddress, assetSocialTestTreasury)
		pyth.RegisterMarkedPot(h.Pyth, pyth.TreasuryRef{GroupID: group.GroupID}, markInput)

		seedAssetSocialHolding(t, h, group.GroupID, session.UserID, label)
	}

	return assetSocialFixture{
		h:        h,
		counting: counting,
		store:    countingStore,
		rpc:      rpc,
		home:     home,
		token:    token,
		userID:   session.UserID,
		groupIDs: groupIDs,
	}
}

// seedAssetSocialHolding gives one club a funded viewer position and a confirmed
// AAPLx buy, so the club really holds the symbol the card asks about.
func seedAssetSocialHolding(t *testing.T, h integrationHarness, groupID, userID, label string) {
	t.Helper()
	db := h.DB
	sfx := h.ISO.Suffix()

	execSQL(t, db,
		`INSERT INTO positions (user_id, group_id, share_units, amount_deposited)
		 VALUES ($1, $2, $3, $3)`,
		userID, groupID, assetSocialTestDeposit)
	execSQL(t, db,
		`INSERT INTO deposits (user_id, group_id, amount, from_address, status, tx_signature)
		 VALUES ($1, $2, $3, 'wallet-'||$4, 'confirmed', $5)`,
		userID, groupID, assetSocialTestDeposit, label, "sig-dep-"+sfx+"-"+label)

	proposalID := queryID(t, db,
		`INSERT INTO proposals (group_id, proposer_id, symbol, usdc_micros, status, expires_at)
		 VALUES ($1, $2, $3, $4, 'passed', now() - interval '1 day') RETURNING id`,
		groupID, userID, assetSocialTestSymbol, assetSocialTestCostUsdc)
	execSQL(t, db,
		`INSERT INTO transactions (group_id, proposal_id, amount, action, input_mint, output_mint,
		   status, tx_signature, execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at)
		 VALUES ($1, $2, $3, 'buy', $4, $5, 'confirmed', $6, $7, $3, $8, now())`,
		groupID, proposalID, assetSocialTestCostUsdc, jupiter.USDCMint, jupiter.AAPLxMint,
		"sig-buy-"+sfx+"-"+label, "req-"+sfx+"-"+label, assetSocialTestUnits)
}

// call runs the endpoint and reports what it cost.
func (fx assetSocialFixture) call(t *testing.T) (AssetSocialResult, int64, int) {
	t.Helper()
	statementsBefore := fx.counting.count()
	rpcBefore := fx.rpc.count()
	result, err := fx.home.GetAssetSocial(context.Background(), fx.token, assetSocialTestSymbol)
	if err != nil {
		t.Fatalf("GetAssetSocial: %v", err)
	}
	return result, fx.counting.since(statementsBefore), fx.rpc.count() - rpcBefore
}

// holdingsWithStatements runs the holdings half on its own and reports the SQL it sent.
func (fx assetSocialFixture) holdingsWithStatements(t *testing.T, symbol string) ([]AssetSocialHolding, int, []string) {
	t.Helper()
	stop := fx.counting.log.start()
	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, symbol)
	return holdings, unvalued, stop()
}

// assetSocialReconcilePerCabal is what one cabal still costs the whole endpoint after
// the holdings read stopped scaling: the deposit reconcile that GET /v1/home/dashboard
// also runs, which checks each treasury for USDC that landed without being credited.
// It is a separate N+1 and is tracked on its own; this test bounds it so that a
// regression in the holdings read cannot hide behind it.
const (
	assetSocialReconcileStatementsPerCabal = 5
	assetSocialReconcileRPCPerCabal        = 1
)

// TestAssetSocialCost_endpointDoesNotScaleWithCabalCount measures the whole request,
// not just the holdings read, so the card's real cost is on the record.
func TestAssetSocialCost_endpointDoesNotScaleWithCabalCount(t *testing.T) {
	const few, many = 1, 5

	oneFx := newAssetSocialFixture(t, few)
	oneResult, oneStatements, oneRPC := oneFx.call(t)
	manyFx := newAssetSocialFixture(t, many)
	manyResult, manyStatements, manyRPC := manyFx.call(t)

	t.Logf("cabals=%d statements=%d rpc=%d", few, oneStatements, oneRPC)
	t.Logf("cabals=%d statements=%d rpc=%d", many, manyStatements, manyRPC)

	if len(oneResult.Holdings) != few || len(manyResult.Holdings) != many {
		t.Fatalf("holdings = %d and %d, want %d and %d",
			len(oneResult.Holdings), len(manyResult.Holdings), few, many)
	}
	if oneResult.UnvaluedGroups != 0 || manyResult.UnvaluedGroups != 0 {
		t.Fatalf("unvalued = %d and %d, want 0", oneResult.UnvaluedGroups, manyResult.UnvaluedGroups)
	}

	extraCabals := int64(many - few)
	if perCabal := (manyStatements - oneStatements) / extraCabals; perCabal > assetSocialReconcileStatementsPerCabal {
		t.Fatalf("each extra cabal costs %d statements, want at most %d (the deposit reconcile alone)",
			perCabal, assetSocialReconcileStatementsPerCabal)
	}
	if perCabal := (manyRPC - oneRPC) / int(extraCabals); perCabal > assetSocialReconcileRPCPerCabal {
		t.Fatalf("each extra cabal costs %d rpc reads, want at most %d (the deposit reconcile alone)",
			perCabal, assetSocialReconcileRPCPerCabal)
	}
}
