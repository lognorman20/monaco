package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	tSpaceXMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	spacexXMint = "SPACExMint1111111111111111111111111111111"
	aaplXMint   = "AAPLxMint11111111111111111111111111111111"
)

func tesseraSpaceX() xstocks.CatalogAsset {
	return xstocks.CatalogAsset{
		Symbol:       "tSpaceX",
		Name:         "T-SpaceX",
		SolanaMint:   tSpaceXMint,
		Kind:         xstocks.AssetKindPreIPO,
		Source:       xstocks.AssetSourceTessera,
		Issuer:       "tessera",
		UnderlyingID: "spacex",
		Decimals:     9,
		Sector:       "Aerospace",
	}.Normalize()
}

func xStockSpaceX() xstocks.CatalogAsset {
	return xstocks.CatalogAssetFromXStockNode("SPACEx", "SpaceX xStock", spacexXMint)
}

func xStockAAPL() xstocks.CatalogAsset {
	return xstocks.CatalogAssetFromXStockNode("AAPLx", "Apple xStock", aaplXMint)
}

func TestCompositeSearch_spacexQuery_returnsTesseraFirst(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockSpaceX())
	tessera := NewFakeSource(tesseraSpaceX())
	prober := xstocks.NewFakeRoutabilityProber(false)
	xstocks.SetRoutable(prober, tSpaceXMint, true)
	xstocks.SetRoutable(prober, spacexXMint, false)

	c := NewComposite(xs, tessera, prober)
	page, err := c.Search(ctx, "spacex", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 {
		t.Fatal("expected at least one asset")
	}
	first := page.Assets[0]
	if first.Kind != xstocks.AssetKindPreIPO || first.Symbol != "tSpaceX" {
		t.Fatalf("first = %+v, want Tessera tSpaceX", first)
	}
}

func TestCompositeSearch_aaplQuery_returnsXStockFirst(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	xstocks.RegisterCatalogAsset(xs, tesseraSpaceX())
	tessera := NewFakeSource(tesseraSpaceX())
	prober := xstocks.NewFakeRoutabilityProber(true)

	c := NewComposite(xs, tessera, prober)
	page, err := c.Search(ctx, "aapl", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 {
		t.Fatal("expected AAPLx")
	}
	if page.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("first symbol = %q, want AAPLx", page.Assets[0].Symbol)
	}
}

func TestCompositeSearch_emptyQuery_appendsPreIpoRows(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)

	page, err := c.Search(ctx, "", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawPreIPO bool
	for _, asset := range page.Assets {
		if asset.Kind == xstocks.AssetKindPreIPO {
			sawPreIPO = true
		}
	}
	if !sawPreIPO {
		t.Fatalf("expected pre-IPO row in %d assets", len(page.Assets))
	}
}

func TestCompositeSearch_emptyQuery_fullXStockPage_keepsPreIpoRows(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)

	page, err := c.Search(ctx, "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) < 2 {
		t.Fatalf("pre-IPO row dropped from a full xStocks page: %d assets", len(page.Assets))
	}
	if page.Assets[len(page.Assets)-1].Kind != xstocks.AssetKindPreIPO {
		t.Fatalf("pre-IPO row = %+v", page.Assets[len(page.Assets)-1])
	}
}

func TestCompositeSearch_tesseraDown_returnsXStocksOnly(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	tessera := NewFakeSource(tesseraSpaceX())
	SetFakeSourceListError(tessera, errors.New("tessera unavailable"))
	c := NewComposite(xs, tessera, nil)

	page, err := c.Search(ctx, "spacex", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range page.Assets {
		if asset.Kind == xstocks.AssetKindPreIPO {
			t.Fatalf("unexpected pre-IPO row when tessera is down: %+v", asset)
		}
	}
}

func TestCompositeSearchKind_preIpo_filtersToTessera(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	xstocks.RegisterCatalogAsset(xs, xStockSpaceX())
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)

	page, err := c.SearchKind(ctx, "", xstocks.AssetKindPreIPO, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 {
		t.Fatal("expected pre-IPO rows")
	}
	for _, asset := range page.Assets {
		if asset.Kind != xstocks.AssetKindPreIPO {
			t.Fatalf("asset %q kind = %q, want pre_ipo", asset.Symbol, asset.Kind)
		}
	}
}

func TestCompositeLookupByMint_returnsExactMint(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockSpaceX())
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)

	asset, ok, err := c.LookupByMint(ctx, tSpaceXMint)
	if err != nil || !ok {
		t.Fatalf("LookupByMint tessera: ok=%v err=%v", ok, err)
	}
	if asset.Symbol != "tSpaceX" {
		t.Fatalf("symbol = %q, want tSpaceX", asset.Symbol)
	}
}

func TestCompositeSearchVariants_returnsAllMints(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	spacexStock := xStockSpaceX()
	spacexStock.UnderlyingID = "spacex"
	xstocks.RegisterCatalogAsset(xs, spacexStock)
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)

	variants, err := c.SearchVariants(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) < 2 {
		t.Fatalf("variants = %d, want at least 2", len(variants))
	}
	mints := make(map[string]bool)
	for _, v := range variants {
		mints[v.SolanaMint] = true
	}
	if !mints[tSpaceXMint] || !mints[spacexXMint] {
		t.Fatalf("missing expected mints: %v", mints)
	}
}

func TestCompositeSearch_spacexQuery_oneRow_variantCount2(t *testing.T) {
	ctx := context.Background()
	c := spacexCompositeWithPrices(t, &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}})
	page, err := c.Search(ctx, "spacex", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var spacexRows int
	for _, a := range page.Assets {
		if underlyingKey(a) == "spacex" {
			spacexRows++
		}
	}
	if spacexRows != 1 {
		t.Fatalf("spacex rows = %d, want 1 collapsed row", spacexRows)
	}
	variants, err := c.SearchVariants(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if len(variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(variants))
	}
}

func TestCompositeSearch_tesseraDisabled_noTesseraRows(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	prestocks := NewFakeSource(prestocksSpaceX())
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: prestocks, SourceID: xstocks.AssetSourcePreStocks},
	}, nil, nil, nil)
	page, err := c.Search(ctx, "spacex", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range page.Assets {
		if asset.Source == xstocks.AssetSourceTessera {
			t.Fatalf("unexpected tessera row: %+v", asset)
		}
	}
}

func TestCompositeSearch_emptyQuery_fullXStockPage_keepsPreStocks(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(xs, xStockAAPL())
	prestocks := NewFakeSource(prestocksSpaceX())
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: prestocks, SourceID: xstocks.AssetSourcePreStocks},
	}, nil, nil, nil)
	page, err := c.Search(ctx, "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) < 2 {
		t.Fatalf("prestocks row dropped from full xStocks page: %d assets", len(page.Assets))
	}
	var sawPreStocks bool
	for _, asset := range page.Assets {
		if asset.Source == xstocks.AssetSourcePreStocks {
			sawPreStocks = true
		}
	}
	if !sawPreStocks {
		t.Fatalf("assets = %+v, want prestocks row", page.Assets)
	}
}

func TestCompositeSearch_spacexQuery_exactMatchCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	xs := xstocks.NewFakeCatalogSearcher()
	tessera := NewFakeSource(tesseraSpaceX())
	c := NewComposite(xs, tessera, nil)
	page, err := c.Search(ctx, strings.ToUpper(tesseraSpaceX().Symbol), 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Assets) == 0 || page.Assets[0].Symbol != "tSpaceX" {
		t.Fatalf("page = %+v", page.Assets)
	}
}
