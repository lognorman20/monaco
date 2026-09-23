package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

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
