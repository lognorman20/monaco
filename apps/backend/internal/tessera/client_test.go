package tessera

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

//go:embed testdata/token-details.json
var tokenDetailsFixture []byte

//go:embed testdata/tokens.json
var tokensFixture []byte

func TestTesseraList_parsesTokenDetailsFixture(t *testing.T) {
	t.Parallel()

	var logoHits atomic.Int32
	srv := newTesseraTestServer(t, &logoHits)
	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())
	t0 := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	catalog.now = func() time.Time { return t0 }

	assets, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(assets) != 3 {
		t.Fatalf("len(assets) = %d, want 3 (malformed mint row dropped)", len(assets))
	}

	bySymbol := map[string]xstocks.CatalogAsset{}
	for _, a := range assets {
		bySymbol[a.Symbol] = a
	}

	spx, ok := bySymbol["tSpaceX"]
	if !ok {
		t.Fatal("missing tSpaceX")
	}
	if spx.Kind != xstocks.AssetKindPreIPO || spx.Decimals != 9 || spx.TransferFeeBps != 20 {
		t.Fatalf("tSpaceX kind/decimals/fee: %+v", spx)
	}
	if spx.SolanaMint != "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v" {
		t.Fatalf("tSpaceX mint: %q", spx.SolanaMint)
	}
	if spx.UnderlyingID != "spacex" {
		t.Fatalf("UnderlyingID = %q, want spacex", spx.UnderlyingID)
	}
	if spx.ReferenceSource != referenceIssuerStale {
		t.Fatalf("ReferenceSource = %q", spx.ReferenceSource)
	}
	if spx.ReferenceMarkUsdcMicros == nil || *spx.ReferenceMarkUsdcMicros != 423_000_000 {
		t.Fatalf("ReferenceMarkUsdcMicros = %v", spx.ReferenceMarkUsdcMicros)
	}
	if spx.LogoURL != "https://cdn.example.test/logo/t-spacex.svg" {
		t.Fatalf("LogoURL = %q", spx.LogoURL)
	}
	if logoHits.Load() != 3 {
		t.Fatalf("logo fetches = %d, want 3", logoHits.Load())
	}
}

func TestTesseraList_cachesFor60s(t *testing.T) {
	t.Parallel()

	var upstreamCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == tokenDetailsPath:
			upstreamCalls.Add(1)
			w.Write(tokenDetailsFixture)
		case r.URL.Path == tokensPath:
			w.Write(tokensFixtureForServer(r))
		case strings.HasSuffix(r.URL.Path, ".json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"image":"https://cdn.example.test/logo/x.svg"}`))
		default:
			http.NotFound(w, r)
		}
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
		t.Fatalf("token-details calls = %d, want 1", got)
	}
}

func TestTesseraList_upstream500_returnsLastGoodThenStaticFallback(t *testing.T) {
	t.Parallel()

	mode := atomic.Int32{} // 0 ok, 1 error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 && (r.URL.Path == tokenDetailsPath || r.URL.Path == tokensPath) {
			http.Error(w, "fail", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == tokenDetailsPath:
			w.Write(tokenDetailsFixture)
		case r.URL.Path == tokensPath:
			w.Write(tokensFixtureForServer(r))
		case strings.HasSuffix(r.URL.Path, ".json"):
			_, _ = w.Write([]byte(`{"image":"https://cdn.example.test/logo/x.svg"}`))
		default:
			http.NotFound(w, r)
		}
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
	if len(good) != 3 {
		t.Fatalf("initial len = %d", len(good))
	}

	mode.Store(1)
	nowNS.Store(t0.Add(2 * time.Minute).UnixNano())
	lastGood, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("List after 500: %v", err)
	}
	if len(lastGood) != 3 {
		t.Fatalf("lastGood len = %d", len(lastGood))
	}
	if lastGood[0].ReferenceMarkUsdcMicros == nil {
		t.Fatal("expected last good to retain reference prices")
	}

	nowNS.Store(t0.Add(25 * time.Hour).UnixNano())
	fallback, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("List static fallback: %v", err)
	}
	if len(fallback) != 3 {
		t.Fatalf("fallback len = %d", len(fallback))
	}
	for _, a := range fallback {
		if a.ReferenceMarkUsdcMicros != nil {
			t.Fatalf("%s still has reference mark on static fallback", a.Symbol)
		}
	}
}

func TestTesseraList_rejectsMalformedMint(t *testing.T) {
	t.Parallel()

	srv := newTesseraTestServer(t, nil)
	catalog := NewHTTPCatalogWithClient(srv.URL, srv.Client())

	assets, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range assets {
		if a.Symbol == "tBad" {
			t.Fatal("malformed mint row was not dropped")
		}
	}
	if len(assets) != 3 {
		t.Fatalf("len = %d, want 3", len(assets))
	}
}

func newTesseraTestServer(t *testing.T, logoHits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == tokenDetailsPath:
			w.Write(tokenDetailsFixture)
		case r.URL.Path == tokensPath:
			w.Write(tokensFixtureForServer(r))
		case strings.HasSuffix(r.URL.Path, ".json"):
			if logoHits != nil {
				logoHits.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			name := strings.TrimSuffix(r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], ".json")
			_, _ = w.Write([]byte(`{"image":"https://cdn.example.test/logo/` + name + `.svg"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tokensFixtureForServer(r *http.Request) []byte {
	base := "http://" + r.Host
	var rows []tokenRow
	if err := json.Unmarshal(tokensFixture, &rows); err != nil {
		panic(err)
	}
	for i := range rows {
		path := strings.TrimPrefix(rows[i].URI, "https://cdn.example.test")
		rows[i].URI = base + path
	}
	out, err := json.Marshal(rows)
	if err != nil {
		panic(err)
	}
	return out
}
