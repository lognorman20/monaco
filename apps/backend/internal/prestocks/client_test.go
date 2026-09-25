package prestocks

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

//go:embed testdata/prestocks.json
var prestocksFixture []byte

func TestPreStocksList_parsesFixture_eightRows_spacexUnderlying(t *testing.T) {
	t.Parallel()

	srv := newPreStocksTestServer(t, prestocksFixture)
	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())
	t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	catalog.now = func() time.Time { return t0 }

	assets, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assets) != 8 {
		t.Fatalf("len(assets) = %d, want 8", len(assets))
	}

	bySymbol := map[string]xstocks.CatalogAsset{}
	for _, a := range assets {
		bySymbol[a.Symbol] = a
		assertReferenceFieldsEmpty(t, a)
	}

	spx, ok := bySymbol["SPACEX"]
	if !ok {
		t.Fatal("missing SPACEX")
	}
	if spx.Kind != xstocks.AssetKindPreIPO || spx.Decimals != 9 || spx.TransferFeeBps != 100 {
		t.Fatalf("SPACEX kind/decimals/fee: %+v", spx)
	}
	if spx.Source != xstocks.AssetSourcePreStocks || spx.Issuer != "prestocks" {
		t.Fatalf("SPACEX source/issuer: %+v", spx)
	}
	if spx.UnderlyingID != "spacex" {
		t.Fatalf("UnderlyingID = %q, want spacex", spx.UnderlyingID)
	}
	if spx.LogoURL != "https://cdn.example.test/spacex.png" {
		t.Fatalf("LogoURL = %q", spx.LogoURL)
	}
}

func TestPreStocksList_nameStripsSuffix(t *testing.T) {
	t.Parallel()

	srv := newPreStocksTestServer(t, prestocksFixture)
	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())

	assets, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range assets {
		if a.Symbol == "SPACEX" {
			if a.Name != "SpaceX" {
				t.Fatalf("Name = %q, want SpaceX", a.Name)
			}
			return
		}
	}
	t.Fatal("SPACEX row not found")
}

func TestPreStocksList_cachesFor60s(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prestocksAPIPath {
			upstreamCalls.Add(1)
			w.Write(prestocksFixture)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())
	t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	var now atomic.Int64
	now.Store(t0.UnixNano())
	catalog.now = func() time.Time { return time.Unix(0, now.Load()) }

	ctx := context.Background()
	if _, err := catalog.List(ctx); err != nil {
		t.Fatalf("first List: %v", err)
	}
	now.Store(t0.Add(30 * time.Second).UnixNano())
	if _, err := catalog.List(ctx); err != nil {
		t.Fatalf("second List: %v", err)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("prestocks API calls = %d, want 1", got)
	}
}

func TestPreStocksList_upstream500_lastGoodThenStatic(t *testing.T) {
	t.Parallel()

	mode := atomic.Int32{} // 0 ok, 1 error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 && r.URL.Path == prestocksAPIPath {
			http.Error(w, "fail", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == prestocksAPIPath {
			w.Write(prestocksFixture)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())
	t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	var nowNS atomic.Int64
	nowNS.Store(t0.UnixNano())
	catalog.now = func() time.Time { return time.Unix(0, nowNS.Load()) }

	ctx := context.Background()
	good, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("initial List: %v", err)
	}
	if len(good) != 8 {
		t.Fatalf("initial len = %d", len(good))
	}

	mode.Store(1)
	nowNS.Store(t0.Add(2 * time.Minute).UnixNano())
	lastGood, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("List after 500: %v", err)
	}
	if len(lastGood) != 8 {
		t.Fatalf("lastGood len = %d", len(lastGood))
	}
	for _, a := range lastGood {
		assertReferenceFieldsEmpty(t, a)
	}

	nowNS.Store(t0.Add(25 * time.Hour).UnixNano())
	fallback, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("List static fallback: %v", err)
	}
	if len(fallback) != 8 {
		t.Fatalf("fallback len = %d", len(fallback))
	}
	for _, a := range fallback {
		assertReferenceFieldsEmpty(t, a)
	}
	if fallback[0].Source != xstocks.AssetSourcePreStocks {
		t.Fatalf("fallback source = %q", fallback[0].Source)
	}
}

func TestPreStocksList_rejectsMalformedMint(t *testing.T) {
	t.Parallel()

	var rows []apiRow
	if err := json.Unmarshal(prestocksFixture, &rows); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	rows = append(rows, apiRow{
		Name:            "Bad PreStocks",
		Symbol:          "BAD",
		ContractAddress: "not-a-valid-mint",
		ExternalURL:     "https://prestocks.com/products/bad",
	})
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	srv := newPreStocksTestServer(t, body)
	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())

	assets, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range assets {
		if a.Symbol == "BAD" {
			t.Fatal("malformed mint row was not dropped")
		}
	}
	if len(assets) != 8 {
		t.Fatalf("len = %d, want 8", len(assets))
	}
}

func newPreStocksTestServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prestocksAPIPath {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertReferenceFieldsEmpty(t *testing.T, a xstocks.CatalogAsset) {
	t.Helper()
	if a.ReferenceMarkUsdcMicros != nil {
		t.Fatalf("%s ReferenceMarkUsdcMicros should be nil", a.Symbol)
	}
	if a.ReferenceValuationUsd != nil {
		t.Fatalf("%s ReferenceValuationUsd should be nil", a.Symbol)
	}
	if a.ReferenceUpdatedAt != nil {
		t.Fatalf("%s ReferenceUpdatedAt should be nil", a.Symbol)
	}
	if a.ReferenceSource != "" {
		t.Fatalf("%s ReferenceSource = %q, want empty", a.Symbol, a.ReferenceSource)
	}
}
