package xstocks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
)

func TestFakeCatalogSearcher_LookupByMint_returnsSymbol(t *testing.T) {
	t.Parallel()

	catalog := NewFakeCatalogSearcher()
	RegisterCatalogAsset(catalog, CatalogAsset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		SolanaMint: jupiter.TSLAxMint,
	})

	asset, found, err := catalog.LookupByMint(context.Background(), jupiter.TSLAxMint)
	if err != nil {
		t.Fatalf("LookupByMint: %v", err)
	}
	if !found {
		t.Fatal("LookupByMint: want found")
	}
	if asset.Symbol != "TSLAx" {
		t.Fatalf("symbol = %q, want TSLAx", asset.Symbol)
	}
}

func TestFakeCatalogSearcher_LookupBySymbol_isCaseInsensitive(t *testing.T) {
	t.Parallel()

	catalog := NewFakeCatalogSearcher()
	RegisterCatalogAsset(catalog, CatalogAsset{
		Symbol:     "TSLAx",
		Name:       "Tesla",
		SolanaMint: jupiter.TSLAxMint,
	})

	asset, found, err := catalog.LookupBySymbol(context.Background(), "  tslax ")
	if err != nil {
		t.Fatalf("LookupBySymbol: %v", err)
	}
	if !found || asset.SolanaMint != jupiter.TSLAxMint {
		t.Fatalf("asset = %+v, found = %v", asset, found)
	}
}

// catalogIndexServer serves a two-page catalogue and counts the page fetches, so
// a test can assert how many times the catalogue was actually crawled.
func catalogIndexServer(t *testing.T, pageFetches *atomic.Int64) *httptest.Server {
	t.Helper()
	nodes := [][]catalogAssetNode{
		{{
			Symbol: "AAPLx",
			Name:   "Apple xStock",
			Logo:   "https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png",
			Deployments: []deployment{
				{Address: jupiter.AAPLxMint, Network: "Solana"},
			},
		}},
		{{
			Symbol: "TSLAx",
			Name:   "Tesla xStock",
			Logo:   "https://xstocks-metadata.backed.fi/logos/tokens/TSLAx.png",
			Deployments: []deployment{
				{Address: jupiter.TSLAxMint, Network: "Solana"},
			},
		}},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v2/public/assets/"
		if strings.TrimPrefix(r.URL.Path, prefix) != "" {
			http.NotFound(w, r)
			return
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 0 || page >= len(nodes) {
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		pageFetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalCatalogListPage(t, nodes[page], page, page < len(nodes)-1))
	}))
}

// The bug this covers: every symbol lookup used to go through Search, and Search
// walked every catalogue page. Twelve held symbols on a polled screen was twelve
// full crawls per tick per viewer.
func TestHTTPCatalogSearcher_manyLookupsCrawlTheCatalogueOnce(t *testing.T) {
	t.Parallel()

	var pageFetches atomic.Int64
	server := catalogIndexServer(t, &pageFetches)
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			symbol := "AAPLx"
			if i%2 == 1 {
				symbol = "TSLAx"
			}
			if _, found, err := searcher.LookupBySymbol(context.Background(), symbol); err != nil || !found {
				t.Errorf("LookupBySymbol(%s) = found %v, err %v", symbol, found, err)
			}
		}(i)
	}
	wg.Wait()

	// Two pages, crawled once. Before the index this was 24 lookups × 2 pages.
	if got := pageFetches.Load(); got != 2 {
		t.Fatalf("catalogue page fetches = %d, want 2", got)
	}
}

// A search is the same data as a lookup, so it must come from the same crawl
// rather than start its own.
func TestHTTPCatalogSearcher_searchReusesTheCrawledIndex(t *testing.T) {
	t.Parallel()

	var pageFetches atomic.Int64
	server := catalogIndexServer(t, &pageFetches)
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())

	for i := 0; i < 5; i++ {
		page, err := searcher.Search(context.Background(), "xStock", 25, 0)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(page.Assets) != 2 {
			t.Fatalf("assets = %d, want 2", len(page.Assets))
		}
	}

	if got := pageFetches.Load(); got != 2 {
		t.Fatalf("catalogue page fetches = %d, want 2", got)
	}
}

// Issue #336's row has a logo on it. The catalogue publishes one on every asset
// payload this package already fetches; before this it was parsed by nothing.
func TestHTTPCatalogSearcher_indexCarriesTheCatalogueLogo(t *testing.T) {
	t.Parallel()

	var pageFetches atomic.Int64
	server := catalogIndexServer(t, &pageFetches)
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())

	asset, found, err := searcher.LookupBySymbol(context.Background(), "AAPLx")
	if err != nil || !found {
		t.Fatalf("LookupBySymbol = found %v, err %v", found, err)
	}
	if asset.LogoURL != "https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png" {
		t.Fatalf("logo = %q, want the catalogue logo", asset.LogoURL)
	}

	byMint, found, err := searcher.LookupByMint(context.Background(), jupiter.TSLAxMint)
	if err != nil || !found {
		t.Fatalf("LookupByMint = found %v, err %v", found, err)
	}
	if byMint.LogoURL == "" {
		t.Fatal("a mint lookup should carry the logo too — it is the same index")
	}
}

// A logo we cannot load is worse than no logo: the row already falls back to its
// ticker tile, and a plain-HTTP or relative URL is a guaranteed broken image.
func TestNormalizeLogoURL_keepsOnlyLoadableURLs(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://cdn.example/a.png":   "https://cdn.example/a.png",
		" https://cdn.example/b.png ": "https://cdn.example/b.png",
		"http://cdn.example/c.png":    "",
		"/logos/d.png":                "",
		"":                            "",
	}
	for raw, want := range cases {
		if got := normalizeLogoURL(raw); got != want {
			t.Errorf("normalizeLogoURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A caller whose own budget expires must not be left waiting on the crawl, and
// must not abandon it either: the next caller should find it finished.
func TestHTTPCatalogSearcher_aCallerDeadlineDoesNotAbandonTheCrawl(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	var pageFetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pageFetches.Add(1) == 1 {
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(marshalCatalogListPage(t, []catalogAssetNode{{
			Symbol:      "AAPLx",
			Name:        "Apple xStock",
			Deployments: []deployment{{Address: jupiter.AAPLxMint, Network: "Solana"}},
		}}, 0, false))
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := searcher.LookupBySymbol(ctx, "AAPLx"); err == nil {
		t.Fatal("a caller whose budget expired before the first crawl should get its deadline error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the caller waited %s past its own 40ms budget", elapsed)
	}

	// The crawl carries on, so the next caller is served from the index.
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		asset, found, err := searcher.LookupBySymbol(context.Background(), "AAPLx")
		if err == nil && found && asset.SolanaMint == jupiter.AAPLxMint {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the abandoned crawl never finished: found %v, err %v", found, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
