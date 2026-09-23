package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/chainlink"
	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

// failingTreasuryRPC fails every treasury balance read, standing in for a Base RPC
// that is down.
type failingTreasuryRPC struct {
	wallets.Client
}

func (f failingTreasuryRPC) TreasuryUSDCBalance(context.Context, string) (int64, error) {
	return 0, errors.New("base rpc unavailable")
}

// TestAssetSocialHoldings_costIsFlatInCabalCount is the regression guard on issue
// #341's "no N+1".
//
// The holdings half is read with set-based queries, so one cabal and a dozen cost the
// same number of statements. If it ever goes back to asking each cabal its own
// question, this fails with the count it grew to.
func TestAssetSocialHoldings_costIsFlatInCabalCount(t *testing.T) {
	one := newAssetSocialFixture(t, 1)
	oneHoldings, oneUnvalued, oneSQL := one.holdingsWithStatements(t, assetSocialTestSymbol)

	many := newAssetSocialFixture(t, 12)
	manyHoldings, manyUnvalued, manySQL := many.holdingsWithStatements(t, assetSocialTestSymbol)

	if len(oneHoldings) != 1 || oneUnvalued != 0 {
		t.Fatalf("one cabal: holdings = %d, unvalued = %d, want 1 and 0", len(oneHoldings), oneUnvalued)
	}
	if len(manyHoldings) != 12 || manyUnvalued != 0 {
		t.Fatalf("twelve cabals: holdings = %d, unvalued = %d, want 12 and 0", len(manyHoldings), manyUnvalued)
	}
	if one.rpc.count() != 0 || many.rpc.count() != 0 {
		t.Fatalf("treasury rpc reads = %d for 1 cabal and %d for 12, want 0: this card is priced from the ledger, not the chain",
			one.rpc.count(), many.rpc.count())
	}
	if len(manySQL) != len(oneSQL) {
		t.Fatalf("statements = %d for 12 cabals and %d for 1, so the read still scales with cabal count.\n12 cabals sent:\n%s",
			len(manySQL), len(oneSQL), formatStatementCounts(manySQL))
	}
	// Each read is one round trip for the whole set. A statement sent twice is a
	// query that has crept back inside the per-cabal loop.
	for statement, sent := range countStatements(manySQL) {
		if sent != 1 {
			t.Errorf("statement sent %d times for 12 cabals, want once: %s", sent, statement)
		}
	}
	if one.marks.count() != many.marks.count() {
		t.Fatalf("mark lookups = %d for 1 cabal and %d for 12, want the same: the mark is per token, not per cabal",
			one.marks.count(), many.marks.count())
	}
	t.Logf("statements=%d for both 1 and 12 cabals, rpc=0, marks=%d:\n%s",
		len(manySQL), many.marks.count(), formatStatementCounts(manySQL))
}

// The card's numbers are the point of the card, so the batched read has to produce
// the same dollars the per-cabal read did: 12 AAPLc bought for $2,600, marked at
// $232.05, all of it the viewer's because she is the only member.
func TestAssetSocialHoldings_valuesTheHoldingAndTheViewersSlice(t *testing.T) {
	fx := newAssetSocialFixture(t, 1)

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if unvalued != 0 {
		t.Fatalf("unvalued = %d, want 0", unvalued)
	}
	if len(holdings) != 1 {
		t.Fatalf("holdings = %d, want 1", len(holdings))
	}

	got := holdings[0]
	for _, want := range []struct{ field, got, want string }{
		{"units", got.Units, "12"},
		{"tokenAmount", got.TokenAmount, "1200000000"},
		{"markUsd", got.MarkUsd, "232.05"},
		{"valueUsd", got.ValueUsd, "2784.60"},
		{"costBasisUsd", got.CostBasisUsd, "2600.00"},
		{"dollarPnl", got.DollarPnL, "+184.60"},
		{"mySliceUsd", got.MySliceUsd, "2784.60"},
	} {
		if want.got != want.want {
			t.Errorf("%s = %q, want %q", want.field, want.got, want.want)
		}
	}
	if got.PercentReturn == nil || *got.PercentReturn != "0.071" {
		t.Errorf("percentReturn = %v, want 0.071", got.PercentReturn)
	}
}

// A cabal the viewer belongs to but has no position in still shows the cabal's
// holding; only the viewer's own slice is zero.
func TestAssetSocialHoldings_memberWithNoPositionSeesTheCabalsHolding(t *testing.T) {
	fx := newAssetSocialFixture(t, 1)
	execSQL(t, fx.h.DB, `DELETE FROM positions WHERE user_id = $1`, fx.userID)

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if unvalued != 0 || len(holdings) != 1 {
		t.Fatalf("holdings = %d, unvalued = %d, want 1 and 0", len(holdings), unvalued)
	}
	if holdings[0].ValueUsd != "2784.60" {
		t.Errorf("valueUsd = %q, want 2784.60", holdings[0].ValueUsd)
	}
	if holdings[0].MySliceUsd != "0.00" || holdings[0].MySlicePercent != "0" {
		t.Errorf("slice = %q / %q, want 0.00 / 0", holdings[0].MySliceUsd, holdings[0].MySlicePercent)
	}
}

// A price outage must not empty the card. Every cabal carries its holding at what it
// paid, which is what the group screens do, and none of them is reported unvalued.
func TestAssetSocialHoldings_priceOutageShowsHoldingsAtCost(t *testing.T) {
	fx := newAssetSocialFixture(t, 3)
	chainlink.RegisterMarkedPotError(fx.h.Marks, marks.TreasuryRef{}, errors.New("chainlink unavailable"))

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if unvalued != 0 {
		t.Fatalf("unvalued = %d, want 0: a cabal valued at cost is still valued", unvalued)
	}
	if len(holdings) != 3 {
		t.Fatalf("holdings = %d, want 3", len(holdings))
	}
	for _, holding := range holdings {
		// $2,600 over 12 units is $216.67 a unit, so the position is worth what it
		// cost and shows no gain.
		if holding.MarkUsd != "216.67" {
			t.Errorf("markUsd = %q, want the cost-basis mark 216.67", holding.MarkUsd)
		}
		if holding.DollarPnL != "+0.00" && holding.DollarPnL != "-0.00" {
			t.Errorf("dollarPnl = %q, want no gain on a cost-basis mark", holding.DollarPnL)
		}
	}
}

// The treasury balance is a live Base read and this card never needed it: the units,
// the cost and the share base are all on the ledger. With the RPC down the card is
// unaffected.
func TestAssetSocialHoldings_survivesATreasuryRPCOutage(t *testing.T) {
	fx := newAssetSocialFixture(t, 2)
	fx.home = NewHomeService(fx.store, fx.h.Auth, failingTreasuryRPC{Client: fx.h.Wallets}, fx.h.Marks, nil, fx.h.Symbols)

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if unvalued != 0 || len(holdings) != 2 {
		t.Fatalf("holdings = %d, unvalued = %d, want 2 and 0 with the rpc down", len(holdings), unvalued)
	}
	if holdings[0].ValueUsd != "2784.60" {
		t.Errorf("valueUsd = %q, want 2784.60", holdings[0].ValueUsd)
	}
}

// A cabal whose units have neither a live price nor a cost basis has no honest
// number. It is counted as unchecked, not shown at zero and not dropped: the other
// cabals still answer.
func TestAssetSocialHoldings_cabalWithNothingToPriceIsCountedNotDropped(t *testing.T) {
	fx := newAssetSocialFixture(t, 2)
	chainlink.RegisterMarkedPotError(fx.h.Marks, marks.TreasuryRef{}, errors.New("chainlink unavailable"))
	// The second cabal's buy records units but no USDC paid, so nothing says what
	// they are worth.
	execSQL(t, fx.h.DB,
		`UPDATE transactions SET cost_basis_price = 0 WHERE group_id = $1 AND action = 'buy'`,
		fx.groupIDs[1])

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if len(holdings) != 1 {
		t.Fatalf("holdings = %d, want the one cabal that can be valued", len(holdings))
	}
	if holdings[0].GroupID != fx.groupIDs[0] {
		t.Errorf("holding group = %s, want %s", holdings[0].GroupID, fx.groupIDs[0])
	}
	if unvalued != 1 {
		t.Fatalf("unvalued = %d, want 1: the card must say a cabal went unchecked", unvalued)
	}
}

// A database failure is the whole set failing, not one cabal. The card keeps its votes
// and its activity and reports every cabal unchecked rather than an empty list, which
// a member would read as "none of my cabals owns this".
func TestAssetSocialHoldings_storeFailureReportsEveryCabalUnchecked(t *testing.T) {
	fx := newAssetSocialFixture(t, 4)
	if err := fx.counting.Close(); err != nil {
		t.Fatalf("close counting db: %v", err)
	}

	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, assetSocialTestSymbol)
	if len(holdings) != 0 {
		t.Fatalf("holdings = %d, want 0 when the read failed", len(holdings))
	}
	if unvalued != 4 {
		t.Fatalf("unvalued = %d, want 4", unvalued)
	}
}

// A symbol none of the viewer's cabals holds costs one query and reports nothing
// unchecked: there is nothing to check.
func TestAssetSocialHoldings_unheldSymbolStopsAfterTheLedgerRead(t *testing.T) {
	fx := newAssetSocialFixture(t, 3)

	mark := fx.counting.count()
	holdings, unvalued := fx.home.assetHoldings(context.Background(), fx.userID, fx.groupIDs, "TSLAc")
	statements := fx.counting.since(mark)

	if len(holdings) != 0 || unvalued != 0 {
		t.Fatalf("holdings = %d, unvalued = %d, want 0 and 0", len(holdings), unvalued)
	}
	if statements != 1 {
		t.Fatalf("statements = %d, want 1: nothing beyond the ledger read is needed", statements)
	}
}
