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
