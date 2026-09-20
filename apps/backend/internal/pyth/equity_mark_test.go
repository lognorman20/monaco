package pyth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestHermesClient_EquityMark_notEntitled403_isEntitlementError(t *testing.T) {
	ClearFeedRegistry()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[{"id":"feed-aapl","market_hours":{"is_open":true}}]`))
			return
		}
		http.Error(w, "Not entitled: feed feed-aapl (no grant accepted)", http.StatusForbidden)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	_, err := client.EquityMark(context.Background(), "AAPLx")

	if !IsEntitlementError(err) {
		t.Fatalf("err = %v, want an entitlement error", err)
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
