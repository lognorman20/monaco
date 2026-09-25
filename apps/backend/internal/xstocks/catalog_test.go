package xstocks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func marshalCatalogListPage(t *testing.T, nodes []catalogAssetNode, currentPage int, hasNext bool) []byte {
	t.Helper()
	payload := catalogListResponse{
		Nodes: nodes,
		Page: catalogPageInfo{
			CurrentPage: currentPage,
			HasNextPage: hasNext,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal catalog list page: %v", err)
	}
	return body
}

func TestHTTPCatalogSearcher_searchWalksPagesBeyondFirst(t *testing.T) {
	t.Parallel()

	var pagesRequested []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		const prefix = "/api/v2/public/assets/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		symbol := strings.TrimPrefix(r.URL.Path, prefix)
		if symbol != "" {
			http.NotFound(w, r)
			return
		}

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page query: %q", r.URL.Query().Get("page"))
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		pagesRequested = append(pagesRequested, page)
		if page > 0 && r.Header.Get("User-Agent") == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		switch page {
		case 0:
			body := marshalCatalogListPage(t, []catalogAssetNode{
				{
					Symbol: "ZZZx",
					Name:   "Zzz Corp xStock",
					Deployments: []deployment{
						{Address: "MintZZZ", Network: solanaNetwork},
					},
				},
			}, 0, true)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		case 1:
			body := marshalCatalogListPage(t, []catalogAssetNode{
				{
					Symbol: "AAPLx",
					Name:   "Apple xStock",
					Deployments: []deployment{
						{Address: aaplxSolanaMint, Network: solanaNetwork},
					},
				},
			}, 1, false)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	page, err := searcher.Search(context.Background(), "Apple", 25, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(page.Assets))
	}
	if page.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", page.Assets[0].Symbol)
	}
	if page.Assets[0].SolanaMint != aaplxSolanaMint {
		t.Fatalf("mint = %q, want %q", page.Assets[0].SolanaMint, aaplxSolanaMint)
	}
	if !slices.Contains(pagesRequested, 1) {
		t.Fatalf("pages requested = %v, want page 1 fetched beyond first page", pagesRequested)
	}
}

func TestHTTPCatalogSearcher_stopsOncePageIsFull(t *testing.T) {
	t.Parallel()

	var pagesRequested []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page query: %q", r.URL.Query().Get("page"))
			http.Error(w, "bad page", http.StatusBadRequest)
			return
		}
		pagesRequested = append(pagesRequested, page)
		symbol := fmt.Sprintf("STK%dx", page)
		body := marshalCatalogListPage(t, []catalogAssetNode{
			{
				Symbol: symbol,
				Name:   "Stock",
				Deployments: []deployment{
					{Address: "Mint" + symbol, Network: solanaNetwork},
				},
			},
		}, page, true)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	page, err := searcher.Search(context.Background(), "Stock", 1, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Assets) != 1 || !page.HasMore {
		t.Fatalf("page = %+v, want one asset and hasMore", page)
	}
	if len(pagesRequested) != 1 || pagesRequested[0] != 0 {
		t.Fatalf("pages requested = %v, want only page 0", pagesRequested)
	}
}

func TestHTTPCatalogSearcher_tickerQuery_resolvesViaSymbolEndpoint(t *testing.T) {
	t.Parallel()

	var listPagesRequested int
	var symbolRequested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		const prefix = "/api/v2/public/assets/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		symbol := strings.TrimPrefix(r.URL.Path, prefix)
		if symbol != "" {
			symbolRequested = symbol
			body := loadFixture(t, "aaplx.json")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}

		listPagesRequested++
		body := marshalCatalogListPage(t, nil, 0, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	page, err := searcher.Search(context.Background(), "AAPL", 25, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if symbolRequested != "AAPLx" {
		t.Fatalf("symbol requested = %q, want AAPLx", symbolRequested)
	}
	if listPagesRequested != 0 {
		t.Fatalf("list pages requested = %d, want 0 for ticker query", listPagesRequested)
	}
	if len(page.Assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(page.Assets))
	}
	if page.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", page.Assets[0].Symbol)
	}
}

func TestHTTPCatalogSearcher_pinsMajorSymbolsOnBrowse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var nodes []catalogAssetNode
		switch page {
		case 0:
			nodes = []catalogAssetNode{
				{
					Symbol: "ZZZx",
					Name:   "Zzz Corp xStock",
					Deployments: []deployment{
						{Address: "MintZZZ", Network: solanaNetwork},
					},
				},
				{
					Symbol: "YYYx",
					Name:   "Yyy Corp xStock",
					Deployments: []deployment{
						{Address: "MintYYY", Network: solanaNetwork},
					},
				},
			}
		case 1:
			nodes = []catalogAssetNode{
				{
					Symbol: "AAPLx",
					Name:   "Apple xStock",
					Deployments: []deployment{
						{Address: aaplxSolanaMint, Network: solanaNetwork},
					},
				},
				{
					Symbol: "TSLAx",
					Name:   "Tesla xStock",
					Deployments: []deployment{
						{Address: "MintTSLA", Network: solanaNetwork},
					},
				},
			}
		}
		body := marshalCatalogListPage(t, nodes, page, page == 0)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	page, err := searcher.Search(context.Background(), "", 2, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(page.Assets))
	}
	if page.Assets[0].Symbol != "AAPLx" || page.Assets[1].Symbol != "TSLAx" {
		t.Fatalf("assets = [%s, %s], want [AAPLx, TSLAx]", page.Assets[0].Symbol, page.Assets[1].Symbol)
	}
}

func TestHTTPCatalogSearcher_searchBoostsPinnedAmongMatches(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := marshalCatalogListPage(t, []catalogAssetNode{
			{
				Symbol: "ZZZx",
				Name:   "Zzz Apple Competitor",
				Deployments: []deployment{
					{Address: "MintZZZ", Network: solanaNetwork},
				},
			},
			{
				Symbol: "AAPLx",
				Name:   "Apple xStock",
				Deployments: []deployment{
					{Address: aaplxSolanaMint, Network: solanaNetwork},
				},
			},
		}, 0, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	page, err := searcher.Search(context.Background(), "apple", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(page.Assets))
	}
	if page.Assets[0].Symbol != "AAPLx" {
		t.Fatalf("first symbol = %q, want AAPLx pinned first", page.Assets[0].Symbol)
	}
}

func TestHTTPCatalogSearcher_ranksRoutableBeforeNonRoutable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := marshalCatalogListPage(t, []catalogAssetNode{
			{
				Symbol: "DEADx",
				Name:   "Dead xStock",
				Deployments: []deployment{
					{Address: "MintDead", Network: solanaNetwork},
				},
			},
			{
				Symbol: "AAPLx",
				Name:   "Apple xStock",
				Deployments: []deployment{
					{Address: aaplxSolanaMint, Network: solanaNetwork},
				},
			},
		}, 0, false)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	prober := NewFakeRoutabilityProber(false)
	SetRoutable(prober, aaplxSolanaMint, true)
	searcher.SetRoutabilityProber(prober)

	page, err := searcher.Search(context.Background(), "x", 10, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(page.Assets))
	}
	if page.Assets[0].Symbol != "AAPLx" || !page.Assets[0].Routable {
		t.Fatalf("first = %+v, want routable AAPLx", page.Assets[0])
	}
	if page.Assets[1].Symbol != "DEADx" || page.Assets[1].Routable {
		t.Fatalf("second = %+v, want non-routable DEADx", page.Assets[1])
	}
}

func TestFakeCatalogSearcher_paginationStableWithRoutability(t *testing.T) {
	t.Parallel()

	searcher := NewFakeCatalogSearcher()
	prober := NewFakeRoutabilityProber(false)
	SetRoutable(prober, "MintB", true)
	SetFakeCatalogRoutabilityProber(searcher, prober)

	RegisterCatalogAsset(searcher, CatalogAsset{Symbol: "AAAx", Name: "A", SolanaMint: "MintA"})
	RegisterCatalogAsset(searcher, CatalogAsset{Symbol: "BBAx", Name: "B", SolanaMint: "MintB"})
	RegisterCatalogAsset(searcher, CatalogAsset{Symbol: "CCAx", Name: "C", SolanaMint: "MintC"})

	first, err := searcher.Search(context.Background(), "", 1, 0)
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	if len(first.Assets) != 1 || first.Assets[0].Symbol != "BBAx" {
		t.Fatalf("first page = %+v, want BBAx", first.Assets)
	}
	if !first.HasMore {
		t.Fatal("expected hasMore on first page")
	}

	second, err := searcher.Search(context.Background(), "", 1, 1)
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if len(second.Assets) != 1 {
		t.Fatalf("second page len = %d, want 1", len(second.Assets))
	}
	if second.Assets[0].Symbol != "AAAx" && second.Assets[0].Symbol != "CCAx" {
		t.Fatalf("second page = %+v, want AAAx or CCAx after routable BBAx", second.Assets[0])
	}
}

func TestHTTPCatalogSearcher_searchPaginatesResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		nodes := []catalogAssetNode{
			{
				Symbol: "AAA" + strconv.Itoa(page) + "x",
				Name:   "Asset " + strconv.Itoa(page),
				Deployments: []deployment{
					{Address: "Mint" + strconv.Itoa(page), Network: solanaNetwork},
				},
			},
		}
		body := marshalCatalogListPage(t, nodes, page, page == 0)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	searcher := NewHTTPCatalogSearcherWithClient(server.URL, server.Client())
	first, err := searcher.Search(context.Background(), "", 1, 0)
	if err != nil {
		t.Fatalf("first Search: %v", err)
	}
	if len(first.Assets) != 1 || first.Assets[0].Symbol != "AAA0x" {
		t.Fatalf("first page = %+v, want AAA0x", first.Assets)
	}
	if !first.HasMore {
		t.Fatal("expected hasMore on first page")
	}

	second, err := searcher.Search(context.Background(), "", 1, 1)
	if err != nil {
		t.Fatalf("second Search: %v", err)
	}
	if len(second.Assets) != 1 || second.Assets[0].Symbol != "AAA1x" {
		t.Fatalf("second page = %+v, want AAA1x", second.Assets)
	}
}
