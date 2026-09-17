package xstocks

import (
	"context"
	"encoding/json"
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
	assets, err := searcher.Search(context.Background(), "Apple")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(assets))
	}
	if assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", assets[0].Symbol)
	}
	if assets[0].SolanaMint != aaplxSolanaMint {
		t.Fatalf("mint = %q, want %q", assets[0].SolanaMint, aaplxSolanaMint)
	}
	if !slices.Contains(pagesRequested, 1) {
		t.Fatalf("pages requested = %v, want page 1 fetched beyond first page", pagesRequested)
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
	assets, err := searcher.Search(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if symbolRequested != "AAPLx" {
		t.Fatalf("symbol requested = %q, want AAPLx", symbolRequested)
	}
	if listPagesRequested != 0 {
		t.Fatalf("list pages requested = %d, want 0 for ticker query", listPagesRequested)
	}
	if len(assets) != 1 {
		t.Fatalf("assets len = %d, want 1", len(assets))
	}
	if assets[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", assets[0].Symbol)
	}
}
