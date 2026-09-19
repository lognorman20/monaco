package pyth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChartSeries_success_returnsMonotonicPoints(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var historicalCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/updates/price/latest":
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/v2/updates/price/"):
			historicalCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer test-pyth-key" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1D)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) < 2 {
		t.Fatalf("expected >=2 points, got %d", len(series.Points))
	}
	if series.RequestedSamples != 25 {
		t.Fatalf("expected 25 requested samples for 1D, got %d", series.RequestedSamples)
	}
	if series.FailedSamples != 0 {
		t.Fatalf("expected 0 failed samples, got %d", series.FailedSamples)
	}
	for i := 1; i < len(series.Points); i++ {
		if series.Points[i].Timestamp <= series.Points[i-1].Timestamp {
			t.Fatalf("expected monotonic timestamps at %d", i)
		}
		if series.Points[i].PriceUsdcMicros <= 0 {
			t.Fatalf("expected positive price at %d", i)
		}
	}
	if historicalCalls.Load() != 25 {
		t.Fatalf("expected 25 historical calls, got %d", historicalCalls.Load())
	}
}

func TestChartSeries_cacheHit_skipsHermesHistorical(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var historicalCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") && !strings.HasSuffix(r.URL.Path, "/latest") {
			historicalCalls.Add(1)
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	ctx := context.Background()

	first, err := client.ChartSeries(ctx, "AAPLx", ChartRange1W)
	if err != nil {
		t.Fatalf("first ChartSeries: %v", err)
	}
	if len(first.Points) < 2 {
		t.Fatalf("expected cached-ready series, got %d points", len(first.Points))
	}
	firstCalls := historicalCalls.Load()

	second, err := client.ChartSeries(ctx, "AAPLx", ChartRange1W)
	if err != nil {
		t.Fatalf("second ChartSeries: %v", err)
	}
	if len(second.Points) != len(first.Points) {
		t.Fatalf("expected same point count from cache, got %d vs %d", len(second.Points), len(first.Points))
	}
	if historicalCalls.Load() != firstCalls {
		t.Fatalf("expected no extra historical calls on cache hit, before=%d after=%d", firstCalls, historicalCalls.Load())
	}
}

func TestFetchHistoricalPriceWithRetry_recoversFrom429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/updates/price/1714746100" {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746100}}]}`))
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	at := time.Unix(1714746100, 0).UTC()
	price, err := client.fetchHistoricalPriceWithRetry(context.Background(), "AAPLx", "feed-aapl", at)
	if err != nil {
		t.Fatalf("fetchHistoricalPriceWithRetry: %v", err)
	}
	if price != 185_000_000 {
		t.Fatalf("expected 185000000 micros, got %d", price)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts (429 then success), got %d", attempts.Load())
	}
}

func TestChartSeries_partialFailuresStillReturnsSeries(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var failRemaining atomic.Int32
	failRemaining.Store(3)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/updates/price/") || strings.HasSuffix(r.URL.Path, "/latest") {
			http.NotFound(w, r)
			return
		}
		if failRemaining.Add(-1) >= 0 {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1W)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) < 2 {
		t.Fatalf("expected partial series with >=2 points, got %d", len(series.Points))
	}
	if series.FailedSamples != 3 {
		t.Fatalf("expected 3 failed samples, got %d", series.FailedSamples)
	}
}

func TestChartSeries_feedNotFound_returnsEmptyReason(t *testing.T) {
	ClearFeedRegistry()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1D)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if series.EmptyReason != "price history unavailable" {
		t.Fatalf("expected empty reason, got %q", series.EmptyReason)
	}
	if len(series.Points) != 0 {
		t.Fatalf("expected no points, got %d", len(series.Points))
	}
}

func TestFetchHistoricalPriceOnce_parsesHermesPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/updates/price/1714746100" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-pyth-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746100}}]}`))
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	at := time.Unix(1714746100, 0).UTC()
	price, status, err := client.fetchHistoricalPriceOnce(context.Background(), "feed-aapl", at)
	if err != nil {
		t.Fatalf("fetchHistoricalPriceOnce: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if price != 185_000_000 {
		t.Fatalf("expected 185000000 micros, got %d", price)
	}
}

func TestChartSeriesCache_expiresEntries(t *testing.T) {
	cache := NewChartSeriesCache(20 * time.Millisecond)
	series := AssetChartSeries{
		Points: []ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 100},
			{Timestamp: 2, PriceUsdcMicros: 200},
		},
		RequestedSamples: 2,
	}
	cache.Set("AAPLx", ChartRange1D, series)

	if _, ok := cache.Get("AAPLx", ChartRange1D); !ok {
		t.Fatal("expected cache hit before expiry")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := cache.Get("AAPLx", ChartRange1D); ok {
		t.Fatal("expected cache miss after expiry")
	}
}

func TestChartSeries_usesCachedFeedIDWithoutPriceFeedLookup(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var feedLookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/price_feeds":
			feedLookups.Add(1)
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/v2/updates/price/"):
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	if _, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1M); err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if feedLookups.Load() != 0 {
		t.Fatalf("expected no feed lookup when feed id cached, got %d", feedLookups.Load())
	}
}

func TestIsRetryableHermesStatus(t *testing.T) {
	cases := map[int]bool{
		http.StatusOK:                  false,
		http.StatusNotFound:            false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
	}
	for status, want := range cases {
		if got := isRetryableHermesStatus(status); got != want {
			t.Fatalf("status %s: got %v want %v", strconv.Itoa(status), got, want)
		}
	}
}
