package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
)

// The token addresses the ledger tests use, read from the pinned catalog rather
// than written as literals so a rename cannot leave a test passing against an
// address the resolver no longer answers with.
func testTokenAddress(t *testing.T, symbol string) string {
	t.Helper()
	address, err := b20.NewPinnedCatalog().ResolveTokenAddress(context.Background(), symbol)
	if err != nil {
		t.Fatalf("resolve %s: %v", symbol, err)
	}
	return address
}

// insertConfirmedSwap records a settled buy or sell against a group's ledger.
func insertConfirmedSwap(t *testing.T, ctx context.Context, db *sql.DB, groupID, action, inputToken, outputToken string, amount, costBasisPrice, costBasisAmount int64, label string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO transactions (group_id, amount, action, input_token, output_token, status,
                          tx_hash, execute_request_id, cost_basis_price, cost_basis_amount, confirmed_at)
VALUES ($1, $2, $3, $4, $5, 'confirmed', $6, $7, $8, $9, now())`,
		groupID, amount, action, inputToken, outputToken, "0x"+label, "req-"+label, costBasisPrice, costBasisAmount)
	if err != nil {
		t.Fatalf("insert confirmed %s: %v", action, err)
	}
}

// A cabal that sold half its position carries half the cost it paid: the card's return
// is measured against what the remaining units cost, not against the whole original
// purchase.
func TestListGroupHoldings_proRatesCostBasisToRemainingUnits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("holdings-prorate"), "Holder")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	groupID := insertOpenGroup(t, ctx, store, iso, user.ID, "Prorate Cabal")
	sfx := iso.Suffix()
	usdc, aapl := dex.USDCAddress(), testTokenAddress(t, "AAPLc")

	// Bought 10 units for $2,000, then sold 4 of them.
	insertConfirmedSwap(t, ctx, db, groupID, "buy", usdc, aapl,
		2_000_000_000, 2_000_000_000, 10_000_000_000, "buy-"+sfx)
	insertConfirmedSwap(t, ctx, db, groupID, "sell", aapl, usdc,
		4_000_000_000, 0, 900_000_000, "sell-"+sfx)

	holdings, err := store.ListGroupHoldings(ctx, []string{groupID})
	if err != nil {
		t.Fatalf("ListGroupHoldings: %v", err)
	}
	if len(holdings) != 1 {
		t.Fatalf("holdings = %d, want 1", len(holdings))
	}
	got := holdings[0]
	if got.Units != 6_000_000_000 {
		t.Errorf("units = %d, want 6000000000", got.Units)
	}
	if !got.HasCostBasis || got.CostBasisUsdc != 1_200_000_000 {
		t.Errorf("cost basis = %d (has=%v), want 1200000000 for the six units left",
			got.CostBasisUsdc, got.HasCostBasis)
	}
}

// A position sold down to nothing is not a holding, and is not reported as one.
func TestListGroupHoldings_dropsFullyExitedPositions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("holdings-exit"), "Seller")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	groupID := insertOpenGroup(t, ctx, store, iso, user.ID, "Exit Cabal")
	sfx := iso.Suffix()
	usdc, aapl := dex.USDCAddress(), testTokenAddress(t, "AAPLc")

	insertConfirmedSwap(t, ctx, db, groupID, "buy", usdc, aapl,
		1_000_000_000, 1_000_000_000, 5_000_000_000, "buy-"+sfx)
	insertConfirmedSwap(t, ctx, db, groupID, "sell", aapl, usdc,
		5_000_000_000, 0, 1_100_000_000, "sell-"+sfx)

	holdings, err := store.ListGroupHoldings(ctx, []string{groupID})
	if err != nil {
		t.Fatalf("ListGroupHoldings: %v", err)
	}
	if len(holdings) != 0 {
		t.Fatalf("holdings = %+v, want none once the position is closed", holdings)
	}
}

// One query answers for several cabals and several tokens at once, and keeps each
// cabal's units with that cabal.
func TestListGroupHoldings_keepsEachCabalsUnitsSeparate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("holdings-many"), "Member")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	first := insertOpenGroup(t, ctx, store, iso, user.ID, "First Cabal")
	second := insertOpenGroup(t, ctx, store, iso, user.ID, "Second Cabal")
	outsider := insertOpenGroup(t, ctx, store, iso, user.ID, "Outsider Cabal")
	sfx := iso.Suffix()
	usdc := dex.USDCAddress()
	aapl, tsla := testTokenAddress(t, "AAPLc"), testTokenAddress(t, "TSLAc")

	insertConfirmedSwap(t, ctx, db, first, "buy", usdc, aapl,
		1_000_000_000, 1_000_000_000, 4_000_000_000, "f-aapl-"+sfx)
	insertConfirmedSwap(t, ctx, db, first, "buy", usdc, tsla,
		500_000_000, 500_000_000, 2_000_000_000, "f-tsla-"+sfx)
	insertConfirmedSwap(t, ctx, db, second, "buy", usdc, aapl,
		300_000_000, 300_000_000, 1_000_000_000, "s-aapl-"+sfx)
	insertConfirmedSwap(t, ctx, db, outsider, "buy", usdc, aapl,
		900_000_000, 900_000_000, 3_000_000_000, "o-aapl-"+sfx)

	holdings, err := store.ListGroupHoldings(ctx, []string{first, second})
	if err != nil {
		t.Fatalf("ListGroupHoldings: %v", err)
	}

	units := make(map[string]int64, len(holdings))
	for _, holding := range holdings {
		units[holding.GroupID+"|"+holding.Token] = holding.Units
	}
	want := map[string]int64{
		first + "|" + aapl:  4_000_000_000,
		first + "|" + tsla:  2_000_000_000,
		second + "|" + aapl: 1_000_000_000,
	}
	for key, wantUnits := range want {
		if units[key] != wantUnits {
			t.Errorf("units[%s] = %d, want %d", key, units[key], wantUnits)
		}
	}
	// A cabal outside the caller's membership list is never in the answer.
	if len(holdings) != len(want) {
		t.Fatalf("holdings = %d, want %d: the read must not widen past the groups asked for",
			len(holdings), len(want))
	}
}

// The share base is SumShareUnitsByGroup and nothing else, for every group in the
// set at once.
//
// It is the same base Home, the group view and the redeem paths divide by. A card
// that added units a redeem job has debited but not yet paid out would put a
// member's slice of one stock a few percent below the same member's figure on Home,
// for the same money. Whatever the right treatment of an in-flight redemption is, it
// has to move for every caller at once, not for this card alone.
func TestListShareBaseForGroups_matchesSumShareUnitsByGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := integrationDB(t)
	iso := prepareIsolation(t, db)
	store := NewStore(db)

	user, err := store.UpsertUser(ctx, iso.UniqueDynamicID("sharebase"), "Holder")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	iso.TrackUser(user.ID)
	withShares := insertOpenGroup(t, ctx, store, iso, user.ID, "Share Cabal")
	empty := insertOpenGroup(t, ctx, store, iso, user.ID, "Empty Cabal")

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := store.IncrementPositionTx(ctx, tx, user.ID, withShares, 4_000_000, 4_000_000); err != nil {
		t.Fatalf("IncrementPositionTx: %v", err)
	}
	// A redemption in flight: debited, not yet paid out. It must not move this base.
	if _, err := store.InsertRedeemJobTx(ctx, tx, user.ID, withShares, 1_000_000, 1_000_000, "0xpayout"); err != nil {
		t.Fatalf("InsertRedeemJobTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	base, err := store.ListShareBaseForGroups(ctx, []string{withShares, empty})
	if err != nil {
		t.Fatalf("ListShareBaseForGroups: %v", err)
	}

	for _, groupID := range []string{withShares, empty} {
		want, err := store.SumShareUnitsByGroup(ctx, groupID)
		if err != nil {
			t.Fatalf("SumShareUnitsByGroup(%s): %v", groupID, err)
		}
		got, present := base[groupID]
		if !present {
			// A missing key and a zero base are different bugs to a caller
			// dividing by this, so every group asked for answers.
			t.Fatalf("group %s missing from the share base, want %d", groupID, want)
		}
		if got != want {
			t.Errorf("share base for %s = %d, want %d: the asset card must divide by the base Home divides by",
				groupID, got, want)
		}
	}
	if base[withShares] != 4_000_000 {
		t.Errorf("share base = %d, want 4000000: an in-flight redeem job is not part of it", base[withShares])
	}
	if base[empty] != 0 {
		t.Errorf("empty cabal share base = %d, want 0", base[empty])
	}
}
