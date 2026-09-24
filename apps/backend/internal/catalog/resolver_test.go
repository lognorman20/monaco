package catalog

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestCompositeResolver_tSpaceX_and_T_SpaceX_resolveSameMint(t *testing.T) {
	tessera := NewFakeSource(tesseraSpaceX())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	resolver := NewResolver(xstocks.NewHTTPResolverWithClient(server.URL, server.Client()), tessera)
	ctx := context.Background()

	m1, err := resolver.ResolveSolanaMint(ctx, "tSpaceX")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := resolver.ResolveSolanaMint(ctx, "T-SpaceX")
	if err != nil {
		t.Fatal(err)
	}
	if m1 != tSpaceXMint || m2 != tSpaceXMint {
		t.Fatalf("mints = %q %q, want %q", m1, m2, tSpaceXMint)
	}
}

func TestCompositeResolver_SPACEX_and_tSpaceX_distinctMints(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}}
	c := spacexCompositeWithPrices(t, prices)
	resolver := NewResolverWithCatalog(xstocks.NewFakeResolver(), c)

	mPre, err := resolver.ResolveSolanaMint(ctx, "SPACEX")
	if err != nil {
		t.Fatal(err)
	}
	mTess, err := resolver.ResolveSolanaMint(ctx, "tSpaceX")
	if err != nil {
		t.Fatal(err)
	}
	if mPre == mTess {
		t.Fatalf("mints equal %q", mPre)
	}
	if mPre != spacexPreStocksMint || mTess != tSpaceXMint {
		t.Fatalf("mPre=%q mTess=%q", mPre, mTess)
	}
	mDefault, err := resolver.ResolveSolanaMint(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if mDefault != tSpaceXMint {
		t.Fatalf("default spacex mint = %q, want tSpaceX best price", mDefault)
	}

	full, err := resolver.ResolveSolanaMint(ctx, "SpaceX PreStocks")
	if err != nil {
		t.Fatal(err)
	}
	if full != spacexPreStocksMint {
		t.Fatalf("SpaceX PreStocks mint = %q, want prestocks", full)
	}
}

func TestCompositeResolver_spacex_usesBestPriceWhenPreStocksListedFirst(t *testing.T) {
	ctx := context.Background()
	prices := &fakePriceClient{prices: map[string]jupiter.TokenPrice{
		tSpaceXMint:         priceEntry(562.19, 500_000, freshStock(746.61, 1.958e12)),
		spacexPreStocksMint: priceEntry(116.74, 100_000, freshStock(149.32, 1.958e12)),
	}}
	xs := xstocks.NewFakeCatalogSearcher()
	c := NewCompositeWithSources(xs, []TaggedSource{
		{Source: NewFakeSource(prestocksSpaceX()), SourceID: xstocks.AssetSourcePreStocks},
		{Source: NewFakeSource(tesseraSpaceX()), SourceID: xstocks.AssetSourceTessera},
	}, xstocks.NewFakeRoutabilityProber(true), mintinfo.NewFakeReader(map[string]mintinfo.Info{
		tSpaceXMint:         {Decimals: 9, TransferFeeBps: 20, UiMultiplier: big.NewRat(1, 1)},
		spacexPreStocksMint: {Decimals: 9, TransferFeeBps: 100, UiMultiplier: big.NewRat(5, 1)},
	}), prices)
	resolver := NewResolverWithCatalog(xstocks.NewFakeResolver(), c)
	got, err := resolver.ResolveSolanaMint(ctx, "spacex")
	if err != nil {
		t.Fatal(err)
	}
	if got != tSpaceXMint {
		t.Fatalf("spacex mint = %q, want tSpaceX even when PreStocks is listed first", got)
	}
}

func TestCompositeResolver_unknown_returnsErrNotFound(t *testing.T) {
	tessera := NewFakeSource()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	resolver := NewResolver(xstocks.NewHTTPResolverWithClient(server.URL, server.Client()), tessera)
	_, err := resolver.ResolveSolanaMint(context.Background(), "NOTREAL")
	if !errors.Is(err, xstocks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
