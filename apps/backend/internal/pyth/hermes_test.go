package pyth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestNewHermesClient_defaultBaseURL_usesUpgradedHost(t *testing.T) {
	client := NewHermesClient("test-pyth-key")
	if client.baseURL != defaultHermesBaseURL {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, defaultHermesBaseURL)
	}
}

func TestHermesClient_fetchLatestPrice_notEntitled403_includesHermesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/updates/price/latest" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-pyth-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Not entitled: feed feed-aapl (no grant accepted)", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	_, err := client.fetchLatestPrice(context.Background(), "feed-aapl")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "status 403") {
		t.Fatalf("error = %q, want status 403", msg)
	}
	if !strings.Contains(msg, "Not entitled") {
		t.Fatalf("error = %q, want Hermes body", msg)
	}
	if !strings.Contains(msg, "Pyth Terminal") {
		t.Fatalf("error = %q, want entitlement hint", msg)
	}
}

func TestHermesClient_fetchPriceFeedBySymbol_sendsBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/price_feeds" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-pyth-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":true}}]`))
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	feed, err := client.fetchPriceFeedBySymbol(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("fetchPriceFeedBySymbol: %v", err)
	}
	if feed.ID != "feed-aapl" {
		t.Fatalf("feed ID = %q", feed.ID)
	}
}

func TestHermesClient_EquityMark_reportsPublishTimeAndSession(t *testing.T) {
	ClearFeedRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":true}}]`))
			return
		}
		_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18550000000","expo":-8,"publish_time":1789830000},"metadata":{"prev_publish_time":1789829999}}]}`))
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	mark, err := client.EquityMark(context.Background(), "AAPLx")

	if err != nil {
		t.Fatalf("EquityMark: %v", err)
	}
	if mark.PriceUsdcMicros != 185_500_000 || !mark.MarketOpen || mark.AfterHours {
		t.Fatalf("mark = %+v", mark)
	}
	if mark.PublishedAt.Unix() != 1789830000 || mark.PublishedAt.Location() != time.UTC {
		t.Fatalf("PublishedAt = %v, want 1789830000 in UTC", mark.PublishedAt)
	}
}

func TestIsEntitlementError_distinguishesDenialFromOutage(t *testing.T) {
	if !IsEntitlementError(fmt.Errorf("wrapped: %w", hermesRequestError("pyth latest price", http.StatusForbidden, []byte("Not entitled")))) {
		t.Fatal("403 should be an entitlement error")
	}
	if IsEntitlementError(hermesRequestError("pyth latest price", http.StatusBadGateway, nil)) {
		t.Fatal("502 should not be an entitlement error")
	}
	if IsEntitlementError(context.DeadlineExceeded) {
		t.Fatal("timeout should not be an entitlement error")
	}
}

func TestHermesClient_ChartSeries_stopsAtFirstEntitlementDenial(t *testing.T) {
	ClearFeedRegistry()
	var historical atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":true}}]`))
			return
		}
		historical.Add(1)
		http.Error(w, "Not entitled: feed feed-aapl (no grant accepted)", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	_, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1M)

	if !IsEntitlementError(err) {
		t.Fatalf("err = %v, want an entitlement error", err)
	}
	if got := historical.Load(); got != 1 {
		t.Fatalf("hermes asked %d times for a denied feed, want 1", got)
	}
}

func TestHermesClient_ChartSeries_outageSamplesStillSkipped(t *testing.T) {
	ClearFeedRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":true}}]`))
			return
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1W)

	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if series.EmptyReason == "" {
		t.Fatal("expected the empty state when every sample fails")
	}
}
