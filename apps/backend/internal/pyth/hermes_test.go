package pyth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHermesClient_markedPot_afterHoursWhenMarketClosed(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/price_feeds":
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":false}}]`))
		case r.URL.Path == "/v2/updates/price/latest":
			if got := r.Header.Get("Authorization"); got != "Bearer test-pyth-key" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			// MCP get_symbols: Equity.US.AAPL/USD uses exponent -5.
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101},"metadata":{"prev_publish_time":1714746101}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	holding := CostBasis{
		Symbol: "AAPLx",
		Mint:   "mint-aapl",
		Units:  100,
		Price:  10_000_000,
		Amount: 100,
	}

	// Act
	nav, err := client.MarkedPot(context.Background(), TreasuryRef{GroupID: "group-1"}, []CostBasis{holding})

	// Assert
	if err != nil {
		t.Fatalf("MarkedPot: %v", err)
	}
	if !nav.AfterHours {
		t.Fatal("expected pot after-hours flag")
	}
	if len(nav.Holdings) != 1 || !nav.Holdings[0].AfterHours {
		t.Fatal("expected holding after-hours flag")
	}
	if nav.Holdings[0].MarkUsdc != 185_000_000 {
		t.Fatalf("expected mark 185000000 micros, got %d", nav.Holdings[0].MarkUsdc)
	}
}

func TestHermesClient_markedPot_refreshesMarketHoursWithCachedFeedID(t *testing.T) {
	// Arrange
	ClearFeedRegistry()
	var marketOpen atomic.Bool
	marketOpen.Store(false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/price_feeds":
			open := marketOpen.Load()
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":` + boolJSON(open) + `}}]`))
		case r.URL.Path == "/v2/updates/price/latest":
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746102},"metadata":{"prev_publish_time":1714746101}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	treasury := TreasuryRef{GroupID: "group-1"}
	holding := CostBasis{Symbol: "AAPLx", Mint: "mint-aapl", Units: 100, Price: 10_000_000, Amount: 100}

	// Act — first mark while session closed
	navClosed, err := client.MarkedPot(context.Background(), treasury, []CostBasis{holding})
	if err != nil {
		t.Fatalf("first MarkedPot: %v", err)
	}

	marketOpen.Store(true)
	navOpen, err := client.MarkedPot(context.Background(), treasury, []CostBasis{holding})

	// Assert
	if err != nil {
		t.Fatalf("second MarkedPot: %v", err)
	}
	if !navClosed.AfterHours {
		t.Fatal("expected after-hours while market closed")
	}
	if navOpen.AfterHours {
		t.Fatal("expected live session after market open; sticky IsOpen cache would fail here")
	}
}

func boolJSON(open bool) string {
	if open {
		return "true"
	}
	return "false"
}

func TestIsFrozenEquityMark_detectsUnchangedPublishTime(t *testing.T) {
	if !isFrozenEquityMark(1714746101, 1714746101) {
		t.Fatal("expected frozen mark")
	}
	if isFrozenEquityMark(1714746101, 1714746100) {
		t.Fatal("expected live mark")
	}
}
