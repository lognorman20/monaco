package catalog

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const spacexPreStocksMint = "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh"

type fakePriceClient struct {
	prices map[string]jupiter.TokenPrice
}

func (f *fakePriceClient) Prices(_ context.Context, mints []string) (map[string]jupiter.TokenPrice, error) {
	out := make(map[string]jupiter.TokenPrice, len(mints))
	for _, mint := range mints {
		if p, ok := f.prices[mint]; ok {
			out[mint] = p
		}
	}
	return out, nil
}

func freshStock(price, mcap float64) *jupiter.StockData {
	return &jupiter.StockData{
		Price:     price,
		Mcap:      mcap,
		UpdatedAt: time.Now().UTC(),
	}
}

func priceEntry(usd float64, liquidity float64, stock *jupiter.StockData) jupiter.TokenPrice {
	return jupiter.TokenPrice{
		PriceUsdcMicros: int64(usd * 1_000_000),
		LiquidityUsd:    liquidity,
		StockData:       stock,
	}
}

func prestocksSpaceX() xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:         "SPACEX",
		Name:           "SpaceX",
		SolanaMint:     spacexPreStocksMint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourcePreStocks,
		Issuer:         "prestocks",
		IssuerName:     "PreStocks",
		UnderlyingID:   "spacex",
		Decimals:       9,
		TransferFeeBps: 100,
	}.Normalize()
}

func spacexCompositeWithPrices(t *testing.T, prices *fakePriceClient) *Composite {
	t.Helper()
	xs := xstocks.NewFakeCatalogSearcher()
	tessera := NewFakeSource(tesseraSpaceX())
	prestocks := NewFakeSource(prestocksSpaceX())
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		tSpaceXMint:         {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1)},
		spacexPreStocksMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: big.NewRat(5, 1)},
	})
	prober := xstocks.NewFakeRoutabilityProber(true)
	return NewCompositeWithSources(xs, []TaggedSource{
		{Source: tessera, SourceID: xstocks.AssetSourceTessera},
		{Source: prestocks, SourceID: xstocks.AssetSourcePreStocks},
	}, prober, mints, prices)
}

func TestBestPrice_spacex_prefersLowerCostRatio(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}}
	c := spacexCompositeWithPrices(t, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Basis != "price_v3" || cmp.ChosenSymbol != "tSpaceX" {
		t.Fatalf("cmp = %+v, want price_v3 tSpaceX", cmp)
	}
}

func TestBestPrice_within25bps_prefersLiquidity(t *testing.T) {
	ctx := context.Background()
	// 10 bps apart on cost ratio; SPACEX has higher liquidity.
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(100, 200_000, freshStock(100, 1.958e12)),
		spacexPreStocksMint: priceEntry(100.10, 900_000, freshStock(100, 1.958e12)),
	}}
	c := spacexCompositeWithPrices(t, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.ChosenSymbol != "SPACEX" {
		t.Fatalf("chosen = %q, want SPACEX (liquidity tiebreak)", cmp.ChosenSymbol)
	}
}

func TestBestPrice_missingReference_fallsBackToLiquidityRule_basisUnavailable(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 900_000, nil),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}}
	tess := tesseraSpaceX()
	tess.LiquidityUsd = 900_000
	xs := xstocks.NewFakeCatalogSearcher()
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		tSpaceXMint:         {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1)},
		spacexPreStocksMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: big.NewRat(5, 1)},
	})
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: NewFakeSource(tess), SourceID: xstocks.AssetSourceTessera},
		{Source: NewFakeSource(prestocksSpaceX()), SourceID: xstocks.AssetSourcePreStocks},
	}, xstocks.NewFakeRoutabilityProber(true), mints, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Basis != "unavailable" {
		t.Fatalf("basis = %q, want unavailable", cmp.Basis)
	}
	if cmp.ChosenSymbol != "tSpaceX" {
		t.Fatalf("chosen = %q, want liquidity fallback tSpaceX", cmp.ChosenSymbol)
	}
}

func TestBestPrice_mcapMismatch_fallsBack(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.90e12)),
	}}
	c := spacexCompositeWithPrices(t, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Basis != "unavailable" {
		t.Fatalf("basis = %q, want unavailable", cmp.Basis)
	}
}

func TestBestPrice_pausedVariant_excluded_reasonIssuerPaused(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}}
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		tSpaceXMint:         {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1), Paused: true},
		spacexPreStocksMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: big.NewRat(5, 1)},
	})
	xs := xstocks.NewFakeCatalogSearcher()
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: NewFakeSource(tesseraSpaceX()), SourceID: xstocks.AssetSourceTessera},
		{Source: NewFakeSource(prestocksSpaceX()), SourceID: xstocks.AssetSourcePreStocks},
	}, xstocks.NewFakeRoutabilityProber(true), mints, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	var pausedReason bool
	for _, cand := range cmp.Candidates {
		if cand.Symbol == "tSpaceX" && cand.Reason == "issuer_paused" {
			pausedReason = true
		}
	}
	if !pausedReason {
		t.Fatalf("candidates = %+v, want tSpaceX issuer_paused", cmp.Candidates)
	}
	if cmp.ChosenSymbol != "SPACEX" {
		t.Fatalf("chosen = %q, want SPACEX", cmp.ChosenSymbol)
	}
}

func TestBestPrice_feeNotAppliedWhileJupiterNetsFee(t *testing.T) {
	ctx := context.Background()
	old := JupiterNetsTransferFee
	JupiterNetsTransferFee = true
	t.Cleanup(func() { JupiterNetsTransferFee = old })

	// ~10 bps price gap; 80 bps fee difference must not flip the winner.
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.85, 100_000, freshStock(149.32, 1.958e12)),
	}}
	mints := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		tSpaceXMint:         {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1)},
		spacexPreStocksMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: big.NewRat(5, 1)},
	})
	xs := xstocks.NewFakeCatalogSearcher()
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: NewFakeSource(tesseraSpaceX()), SourceID: xstocks.AssetSourceTessera},
		{Source: NewFakeSource(prestocksSpaceX()), SourceID: xstocks.AssetSourcePreStocks},
	}, xstocks.NewFakeRoutabilityProber(true), mints, prices)
	cmp, err := c.Compare(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if cmp.ChosenSymbol != "tSpaceX" {
		t.Fatalf("chosen = %q, want tSpaceX when fees are not applied", cmp.ChosenSymbol)
	}
}
