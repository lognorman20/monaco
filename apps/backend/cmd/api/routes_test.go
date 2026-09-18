package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/httpapi"
)

func TestProductionRoutesSmoke(t *testing.T) {
	// Missing authentication must be rejected before any provider or database call.
	mux, patterns := newAPIMux(routeHandlers{
		Auth: &httpapi.AuthHandlers{}, Me: &httpapi.MeHandlers{},
		Home: &httpapi.HomeHandlers{}, GroupsTab: &httpapi.GroupsTabHandlers{},
		Groups: &httpapi.GroupHandlers{}, Deposits: &httpapi.DepositHandlers{},
		Transactions: &httpapi.TransactionHandlers{}, Catalog: &httpapi.CatalogHandlers{},
		Assets: &httpapi.AssetsHandlers{}, Quotes: &httpapi.QuoteHandlers{},
		Proposals: &httpapi.ProposalHandlers{},
	})
	required := map[string]bool{
		"GET /v1/home/dashboard": false, "GET /v1/home/pnl-series": false,
		"GET /v1/home/missed-proposals": false, "GET /v1/groups/search": false,
		"GET /v1/groups/leaderboard": false, "GET /v1/groups/{id}/pnl-history": false,
		"GET /v1/assets": false, "GET /v1/assets/popular": false,
		"GET /v1/assets/{symbol}": false, "GET /v1/assets/{symbol}/chart": false,
	}
	for _, pattern := range patterns {
		if _, ok := required[pattern]; ok {
			required[pattern] = true
		}
		t.Run(pattern, func(t *testing.T) {
			method, path, _ := strings.Cut(pattern, " ")
			path = strings.NewReplacer("{id}", "11111111-1111-1111-1111-111111111111", "{symbol}", "AAPL", "{requestId}", "22222222-2222-2222-2222-222222222222").Replace(path)
			req := httptest.NewRequest(method, path, nil)
			_, matched := mux.Handler(req)
			if matched != pattern {
				t.Fatalf("matched %q, want %q", matched, pattern)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			want := http.StatusUnauthorized
			if pattern == "GET /health" {
				want = http.StatusOK
			}
			if pattern == "POST /v1/auth/session" {
				want = http.StatusBadRequest
			}
			if response.Code != want {
				t.Fatalf("status %d, want %d: %s", response.Code, want, response.Body.String())
			}
		})
	}
	for pattern, found := range required {
		if !found {
			t.Errorf("required shell route missing: %s", pattern)
		}
	}
}
