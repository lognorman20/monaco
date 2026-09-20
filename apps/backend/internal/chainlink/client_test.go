package chainlink

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/marks"
)

func TestChainlink_MarkedPot_convertsEightDecimalsToMicros(t *testing.T) {
	chain := evm.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	b20.RegisterAsset(catalog, b20.Asset{Symbol: "AAPLc", TokenAddress: "0xaaa", FeedAddress: "0xfeed", Decimals: 8})
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	chain.SetRoundData("0xfeed", evm.RoundData{Answer: big.NewInt(18_500_000_000), UpdatedAt: now}) // $185.00
	c := NewClient(chain, catalog, func() time.Time { return now })
	nav, err := c.MarkedPot(context.Background(), marks.TreasuryRef{Address: "0xt", TreasuryUsdc: 1}, []marks.CostBasis{
		{Symbol: "AAPLc", Token: "0xaaa", Units: 100_000_000, Amount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nav.Holdings) != 1 {
		t.Fatalf("holdings = %d", len(nav.Holdings))
	}
	// units 1e8 * priceMicros 185_000_000 / 1e8 = 185_000_000
	if nav.Holdings[0].MarkUsdc != 185_000_000 {
		t.Fatalf("mark = %d, want 185000000", nav.Holdings[0].MarkUsdc)
	}
}

func TestChainlink_MarkedPot_staleFeedSetsAfterHours(t *testing.T) {
	chain := evm.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	b20.RegisterAsset(catalog, b20.Asset{Symbol: "AAPLc", TokenAddress: "0xaaa", FeedAddress: "0xfeed", Decimals: 8})
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	chain.SetRoundData("0xfeed", evm.RoundData{Answer: big.NewInt(1_000_000_000), UpdatedAt: now.Add(-26 * time.Hour)})
	c := NewClient(chain, catalog, func() time.Time { return now })
	nav, err := c.MarkedPot(context.Background(), marks.TreasuryRef{Address: "0xt"}, []marks.CostBasis{
		{Symbol: "AAPLc", Token: "0xaaa", Units: 1, Amount: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nav.AfterHours || !nav.Holdings[0].AfterHours {
		t.Fatalf("after hours = %+v", nav)
	}
}

func TestChainlink_zeroAnswer_returnsErrMarkUnavailable(t *testing.T) {
	chain := evm.NewFakeClient()
	catalog := b20.NewFakeCatalog()
	b20.RegisterAsset(catalog, b20.Asset{Symbol: "AAPLc", TokenAddress: "0xaaa", FeedAddress: "0xfeed", Decimals: 8})
	chain.SetRoundData("0xfeed", evm.RoundData{Answer: big.NewInt(0), UpdatedAt: time.Now()})
	c := NewClient(chain, catalog, time.Now)
	_, err := c.MarkedPot(context.Background(), marks.TreasuryRef{Address: "0xt"}, []marks.CostBasis{
		{Symbol: "AAPLc", Token: "0xaaa", Units: 1, Amount: 1},
	})
	if err != marks.ErrMarkUnavailable {
		t.Fatalf("err = %v, want ErrMarkUnavailable", err)
	}
}
