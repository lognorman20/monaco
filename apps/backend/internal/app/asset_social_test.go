package app

import (
	"database/sql"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

func TestAssetHoldingRow_valuesUnitsAndReturn(t *testing.T) {
	t.Parallel()

	// 12 AAPLx at $232.05, bought for $2,600.00.
	build, err := assetHoldingRow("Weekend investors", "g1", pyth.MarkedHolding{
		Symbol:    "AAPLx",
		Units:     12 * 100_000_000,
		MarkUsdc:  232_050_000,
		CostBasis: 2_600_000_000,
	})
	if err != nil {
		t.Fatalf("assetHoldingRow: %v", err)
	}

	if build.row.Units != "12" {
		t.Fatalf("units = %q, want 12", build.row.Units)
	}
	if build.row.ValueUsd != "2784.60" {
		t.Fatalf("valueUsd = %q, want 2784.60", build.row.ValueUsd)
	}
	if build.row.CostBasisUsd != "2600.00" {
		t.Fatalf("costBasisUsd = %q, want 2600.00", build.row.CostBasisUsd)
	}
	if build.row.DollarPnL != "+184.60" {
		t.Fatalf("dollarPnl = %q, want +184.60", build.row.DollarPnL)
	}
	if build.row.PercentReturn == nil {
		t.Fatal("expected a percent return when there is a cost basis")
	}
	if got := *build.row.PercentReturn; got != "0.071" {
		t.Fatalf("percentReturn = %q, want 0.071", got)
	}
}

// A pre-IPO token is valued at its own nine decimals. At an xStock's eight the
// cabal's position would read ten times too large.
func TestAssetHoldingRow_valuesAPreIPOHoldingAtItsOwnDecimals(t *testing.T) {
	t.Parallel()

	// 3 tSpaceX at $774.00, bought for $1,500.00.
	build, err := assetHoldingRow("Moonshots", "g1", pyth.MarkedHolding{
		Symbol:       "tSpaceX",
		Units:        3 * 1_000_000_000,
		MarkUsdc:     774_000_000,
		CostBasis:    1_500_000_000,
		Decimals:     9,
		Kind:         xstocks.AssetKindPreIPO,
		UiMultiplier: big.NewRat(1, 1),
	})
	if err != nil {
		t.Fatalf("assetHoldingRow: %v", err)
	}
	if build.row.Units != "3" {
		t.Fatalf("units = %q, want 3", build.row.Units)
	}
	if build.row.ValueUsd != "2322.00" {
		t.Fatalf("valueUsd = %q, want 2322.00", build.row.ValueUsd)
	}
	if build.valueMicros != 2_322_000_000 {
		t.Fatalf("valueMicros = %d, want 2322000000", build.valueMicros)
	}
}

// A position with no cost basis — migrated or airdropped units — has a value but no
// return, and must not be reported as a 100% gain.
func TestAssetHoldingRow_noCostBasis_hasNoPercentReturn(t *testing.T) {
	t.Parallel()

	build, err := assetHoldingRow("Weekend investors", "g1", pyth.MarkedHolding{
		Symbol:   "AAPLx",
		Units:    100_000_000,
		MarkUsdc: 232_050_000,
	})
	if err != nil {
		t.Fatalf("assetHoldingRow: %v", err)
	}
	if build.row.PercentReturn != nil {
		t.Fatalf("percentReturn = %v, want nil", *build.row.PercentReturn)
	}
	if build.row.DollarPnL != "+232.05" {
		t.Fatalf("dollarPnl = %q, want +232.05", build.row.DollarPnL)
	}
}

func TestAssetHoldingRow_lossKeepsItsSign(t *testing.T) {
	t.Parallel()

	build, err := assetHoldingRow("Weekend investors", "g1", pyth.MarkedHolding{
		Symbol:    "AAPLx",
		Units:     100_000_000,
		MarkUsdc:  200_000_000,
		CostBasis: 250_000_000,
	})
	if err != nil {
		t.Fatalf("assetHoldingRow: %v", err)
	}
	if build.row.DollarPnL != "-50.00" {
		t.Fatalf("dollarPnl = %q, want -50.00", build.row.DollarPnL)
	}
	if got := *build.row.PercentReturn; got != "-0.2" {
		t.Fatalf("percentReturn = %q, want -0.2", got)
	}
}

func TestUSDDecimalLess_ordersByCentsNotByString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want bool
	}{
		{"9.000000", "10.000000", true},
		{"10.000000", "9.000000", false},
		{"100.250000", "100.750000", true},
		{"100.750000", "100.250000", false},
		{"0.000000", "0.000000", false},
		// A pot larger than float64 can hold to the cent still orders exactly.
		{"9007199254740993.000000", "9007199254740994.000000", true},
		{"-5.000000", "1.000000", true},
	}
	for _, tc := range cases {
		if got := usdDecimalLess(tc.a, tc.b); got != tc.want {
			t.Fatalf("usdDecimalLess(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAssetActivityFeed_newestFirstAndFillAboveItsProposal(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	proposals := []postgres.SymbolProposalRow{
		{
			ProposalRow: postgres.ProposalRow{
				ID: "p-old", GroupID: "g1", Symbol: "AAPLx",
				Kind: domain.ProposalKindBuy, Status: domain.ProposalPassed,
				UsdcMicros: 5_000_000, CreatedAt: at.Add(-2 * time.Hour),
			},
			GroupName: "Weekend investors",
		},
		{
			ProposalRow: postgres.ProposalRow{
				ID: "p-new", GroupID: "g1", Symbol: "AAPLx",
				Kind: domain.ProposalKindSell, Status: domain.ProposalOpen,
				TokenAmount: 100_000_000, CreatedAt: at,
			},
			GroupName: "Weekend investors",
		},
	}
	fills := []postgres.SymbolFillRow{
		// Same instant as the proposal it settles: the fill is the later fact.
		{ID: "t-1", GroupID: "g1", GroupName: "Weekend investors", Action: "buy", Status: "confirmed", Amount: 5_000_000, CreatedAt: at.Add(-2 * time.Hour)},
		// Still in flight: the proposal's own line already covers it.
		{ID: "t-2", GroupID: "g1", GroupName: "Weekend investors", Action: "buy", Status: "pending", Amount: 1_000_000, CreatedAt: at.Add(-time.Minute)},
	}

	feed := assetActivityFeed(proposals, fills, 10)

	if len(feed) != 3 {
		t.Fatalf("feed length = %d, want 3 (a pending swap is not a fill); feed = %+v", len(feed), feed)
	}
	if feed[0].ID != "p-new" || feed[0].Kind != AssetActivityProposed {
		t.Fatalf("first item = %+v, want the newest open proposal", feed[0])
	}
	if feed[1].ID != "t-1" || feed[1].Kind != AssetActivityFilled {
		t.Fatalf("second item = %+v, want the fill above the proposal it settled", feed[1])
	}
	if feed[2].ID != "p-old" || feed[2].Kind != AssetActivityPassed {
		t.Fatalf("third item = %+v, want the passed proposal", feed[2])
	}
}

func TestAssetActivityFeed_sellReportsProceedsNotTokenAtomics(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	// The cabal sold 12 AAPLx for $2,784.60. transactions.amount holds the input
	// side of the swap, which for a sell is the token: 12 * 1e8 atomics. Read as
	// micros that is "$1,200.00", a number nobody's money ever touched.
	const soldAtomics = 12 * jupiter.XStockAtomicScale
	const proceedsMicros = int64(2_784_600_000)

	fills := []postgres.SymbolFillRow{
		{
			ID: "t-sell", GroupID: "g1", GroupName: "Weekend investors",
			Action: "sell", Status: "confirmed",
			Amount:          soldAtomics,
			CostBasisAmount: sql.NullInt64{Int64: proceedsMicros, Valid: true},
			TokenAmount:     soldAtomics,
			CreatedAt:       at,
		},
		// An older sell that never recorded proceeds has no dollar figure at all.
		// Zero lets the app fall back to the share count instead of inventing one.
		{
			ID: "t-sell-legacy", GroupID: "g1", GroupName: "Weekend investors",
			Action: "sell", Status: "confirmed",
			Amount:      soldAtomics,
			TokenAmount: soldAtomics,
			CreatedAt:   at.Add(-time.Hour),
		},
	}

	feed := assetActivityFeed(nil, fills, 10)
	if len(feed) != 2 {
		t.Fatalf("feed length = %d, want 2; feed = %+v", len(feed), feed)
	}
	if feed[0].UsdcMicros != proceedsMicros {
		t.Fatalf("sell usdcMicros = %d, want the %d micros of proceeds (a sell's amount column is token atomics)", feed[0].UsdcMicros, proceedsMicros)
	}
	if feed[0].TokenAmount != soldAtomics {
		t.Fatalf("sell tokenAmount = %d, want %d", feed[0].TokenAmount, soldAtomics)
	}
	if feed[1].UsdcMicros != 0 {
		t.Fatalf("sell without recorded proceeds reported %d micros, want 0 so the row shows shares", feed[1].UsdcMicros)
	}
}

func TestAssetActivityFeed_respectsLimitAndUTC(t *testing.T) {
	t.Parallel()

	// A timestamp in a non-UTC zone must be normalised: the app formats in the
	// reader's zone and would otherwise be handed the server's.
	zone := time.FixedZone("UTC-7", -7*3600)
	at := time.Date(2026, 9, 22, 7, 0, 0, 0, zone)

	rows := make([]postgres.SymbolProposalRow, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, postgres.SymbolProposalRow{
			ProposalRow: postgres.ProposalRow{
				ID: string(rune('a' + i)), GroupID: "g1", Symbol: "AAPLx",
				Kind: domain.ProposalKindBuy, Status: domain.ProposalOpen,
				CreatedAt: at.Add(time.Duration(i) * time.Minute),
			},
		})
	}

	feed := assetActivityFeed(rows, nil, 2)
	if len(feed) != 2 {
		t.Fatalf("feed length = %d, want 2", len(feed))
	}
	if feed[0].CreatedAt.Location() != time.UTC {
		t.Fatalf("createdAt zone = %v, want UTC", feed[0].CreatedAt.Location())
	}
	if !feed[0].CreatedAt.Equal(at.Add(4 * time.Minute)) {
		t.Fatalf("first item at %v, want the newest", feed[0].CreatedAt)
	}
}

func TestActivityKindForStatus(t *testing.T) {
	t.Parallel()

	cases := map[domain.ProposalStatus]string{
		domain.ProposalOpen:    AssetActivityProposed,
		domain.ProposalPassed:  AssetActivityPassed,
		domain.ProposalFailed:  AssetActivityFailed,
		domain.ProposalExpired: AssetActivityExpired,
	}
	for status, want := range cases {
		if got := activityKindForStatus(status); got != want {
			t.Fatalf("activityKindForStatus(%q) = %q, want %q", status, got, want)
		}
	}
}
