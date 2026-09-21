package chainlink

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestAssetPrices_AssetMark_usesChainlinkAnswer(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed, err := catalog.Feed(context.Background(), "TSLAc")
	if err != nil {
		t.Fatal(err)
	}
	chain := evm.NewFakeClient()
	// $248.50 with 8 decimal Chainlink answer.
	chain.SetRoundData(feed, evm.RoundData{
		Answer:    big.NewInt(24_850_000_000),
		UpdatedAt: time.Now(),
	})
	prices := NewAssetPrices(chain, catalog, time.Now)
	mark, err := prices.AssetMark(context.Background(), "TSLAc")
	if err != nil {
		t.Fatal(err)
	}
	if mark.PriceUsdcMicros != 248_500_000 {
		t.Fatalf("price = %d, want 248500000", mark.PriceUsdcMicros)
	}
}

func TestAssetPrices_AssetMarks_fillsEveryPinnedSymbol(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	chain := evm.NewFakeClient()
	assets, err := catalog.Popular(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	symbols := make([]string, 0, len(assets))
	for i, a := range assets {
		feed, err := catalog.Feed(context.Background(), a.Symbol)
		if err != nil {
			t.Fatal(err)
		}
		chain.SetRoundData(feed, evm.RoundData{
			Answer:    big.NewInt(int64((i + 1) * 10_000_000_000)),
			UpdatedAt: time.Now(),
		})
		symbols = append(symbols, a.Symbol)
	}
	prices := NewAssetPrices(chain, catalog, time.Now)
	marks, err := prices.AssetMarks(context.Background(), symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != len(symbols) {
		t.Fatalf("marks = %d, want %d (%v)", len(marks), len(symbols), marks)
	}
}

func TestAssetPrices_ChartSeries_usesRoundHistory(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed, err := catalog.Feed(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	chain := evm.NewFakeClient()
	chain.SetRoundHistory(feed, []evm.RoundData{
		{Answer: big.NewInt(18_000_000_000), UpdatedAt: now.Add(-2 * time.Hour)},
		{Answer: big.NewInt(19_000_000_000), UpdatedAt: now.Add(-time.Hour)},
		{Answer: big.NewInt(20_000_000_000), UpdatedAt: now},
	})
	prices := NewAssetPrices(chain, catalog, func() time.Time { return now })
	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 3 {
		t.Fatalf("points = %d, want 3 (%+v)", len(series.Points), series)
	}
	if series.Points[2].PriceUsdcMicros != 200_000_000 {
		t.Fatalf("last price = %d", series.Points[2].PriceUsdcMicros)
	}
	// Rounds are the token per token, not the equity: the series has to say so.
	if series.Source != pyth.ChartSourceChainlink || series.Basis != pyth.PriceBasisToken || series.BasisSymbol != "AAPLc" {
		t.Fatalf("source/basis/symbol = %q/%q/%q", series.Source, series.Basis, series.BasisSymbol)
	}
	if series.Range != pyth.ChartRange1D || series.PreviousCloseUsdcMicros != nil {
		t.Fatalf("range = %q, previous close = %v", series.Range, series.PreviousCloseUsdcMicros)
	}
}

func TestAssetPrices_ChartSeries_longRangesAreEmptyNotRelabelledRounds(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed, err := catalog.Feed(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	chain := evm.NewFakeClient()
	chain.SetRoundHistory(feed, []evm.RoundData{
		{Answer: big.NewInt(19_000_000_000), UpdatedAt: now.Add(-48 * time.Hour)},
		{Answer: big.NewInt(20_000_000_000), UpdatedAt: now},
	})
	prices := NewAssetPrices(chain, catalog, func() time.Time { return now })
	for _, chartRange := range []pyth.ChartRange{pyth.ChartRange3M, pyth.ChartRange1Y, pyth.ChartRangeAll} {
		series, err := prices.ChartSeries(context.Background(), "AAPLc", chartRange)
		if err != nil {
			t.Fatal(err)
		}
		if len(series.Points) != 0 || series.EmptyReason != pyth.EmptyReasonNoHistory || series.Range != chartRange {
			t.Fatalf("%s: series = %+v, want empty — two days of rounds are not a %s chart", chartRange, series, chartRange)
		}
	}
}

func TestAssetPrices_ChartSeries_neverDrawsRoundsOutsideTheWindow(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed, err := catalog.Feed(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	chain := evm.NewFakeClient()
	// One round inside the last day, the rest a week old: the 1D chart used to fall
	// back to every round and ship a week as a day.
	chain.SetRoundHistory(feed, []evm.RoundData{
		{Answer: big.NewInt(17_000_000_000), UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		{Answer: big.NewInt(18_000_000_000), UpdatedAt: now.Add(-7 * 24 * time.Hour)},
		{Answer: big.NewInt(20_000_000_000), UpdatedAt: now.Add(-time.Hour)},
	})
	prices := NewAssetPrices(chain, catalog, func() time.Time { return now })
	day, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Points) != 0 {
		t.Fatalf("1D points = %+v, want none — one round in the window is not a chart", day.Points)
	}
	month, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1M)
	if err != nil {
		t.Fatal(err)
	}
	if len(month.Points) != 3 {
		t.Fatalf("1M points = %d, want 3", len(month.Points))
	}
}

func TestAssetPrices_AssetMark_acceptsLegacyXSuffix(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed, err := catalog.Feed(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	chain := evm.NewFakeClient()
	chain.SetRoundData(feed, evm.RoundData{
		Answer:    big.NewInt(20_000_000_000),
		UpdatedAt: time.Now(),
	})
	prices := NewAssetPrices(chain, catalog, time.Now)
	mark, err := prices.AssetMark(context.Background(), "AAPLx")
	if err != nil {
		t.Fatal(err)
	}
	if mark.PriceUsdcMicros != 200_000_000 {
		t.Fatalf("price = %d, want 200000000", mark.PriceUsdcMicros)
	}
}
