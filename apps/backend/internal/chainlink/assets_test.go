package chainlink

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
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

func TestAssetPrices_AssetMark_carriesTheRoundTimeAndTheHeartbeatVerdict(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	fresh, err := catalog.Feed(context.Background(), "AAPLc")
	if err != nil {
		t.Fatal(err)
	}
	old, err := catalog.Feed(context.Background(), "TSLAc")
	if err != nil {
		t.Fatal(err)
	}
	// Monday 09:00 ET: AAPLc's round is from an hour ago, TSLAc's from Friday's
	// close, which is past the feed's 25-hour heartbeat.
	now := time.Date(2026, time.September, 21, 13, 0, 0, 0, time.UTC)
	friday := time.Date(2026, time.September, 18, 20, 0, 0, 0, time.UTC)
	chain := evm.NewFakeClient()
	chain.SetRoundData(fresh, evm.RoundData{Answer: big.NewInt(23_205_000_000), UpdatedAt: now.Add(-time.Hour)})
	chain.SetRoundData(old, evm.RoundData{Answer: big.NewInt(24_850_000_000), UpdatedAt: friday})
	prices := NewAssetPrices(chain, catalog, func() time.Time { return now })

	single, err := prices.AssetMark(context.Background(), "TSLAc")
	if err != nil {
		t.Fatal(err)
	}
	if !single.UpdatedAt.Equal(friday) || !single.AfterHours {
		t.Fatalf("TSLAc mark = %+v, want Friday's round time and after-hours", single)
	}

	marks, err := prices.AssetMarks(context.Background(), []string{"AAPLc", "TSLAc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := marks["AAPLc"]; !got.UpdatedAt.Equal(now.Add(-time.Hour)) || got.AfterHours {
		t.Fatalf("AAPLc mark = %+v, want the round time an hour ago and not after-hours", got)
	}
	if got := marks["TSLAc"]; !got.UpdatedAt.Equal(friday) || !got.AfterHours {
		t.Fatalf("TSLAc mark = %+v, want Friday's round time and after-hours", got)
	}
	if got := marks["TSLAc"].UpdatedAt.Location(); got != time.UTC {
		t.Fatalf("round time location = %v, want UTC", got)
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
