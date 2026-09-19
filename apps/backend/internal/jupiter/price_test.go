package jupiter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPPriceClient_Prices_sendsIDsAndAPIKey(t *testing.T) {
	t.Parallel()

	var gotQuery string
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("ids")
		gotAPIKey = r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{
			"XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp": {"usdPrice": 185.5, "priceChange24h": 1.25}
		}`))
	}))
	defer server.Close()

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "test-jup-key")
	prices, err := client.Prices(context.Background(), []string{"XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"})

	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if gotQuery != "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp" {
		t.Fatalf("ids query = %q", gotQuery)
	}
	if gotAPIKey != "test-jup-key" {
		t.Fatalf("x-api-key = %q, want test-jup-key", gotAPIKey)
	}
	price, ok := prices["XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"]
	if !ok {
		t.Fatal("expected a price for the requested mint")
	}
	// 185.5 USD -> 185500000 micros.
	if price.PriceUsdcMicros != 185_500_000 {
		t.Fatalf("PriceUsdcMicros = %d, want 185500000", price.PriceUsdcMicros)
	}
	// Jupiter reports a percentage (1.25 = +1.25%); the contract here is a ratio.
	if price.Change24h == nil || *price.Change24h != "0.012500" {
		t.Fatalf("Change24h = %v, want 0.012500", price.Change24h)
	}
}

func TestHTTPPriceClient_Prices_noAPIKey_omitsHeader(t *testing.T) {
	t.Parallel()

	var sawHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHeader = r.Header.Get("x-api-key") != ""
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "")
	if _, err := client.Prices(context.Background(), []string{"mint1"}); err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if sawHeader {
		t.Fatal("expected no x-api-key header when apiKey is empty")
	}
}

func TestHTTPPriceClient_Prices_missingMint_omittedFromResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Jupiter simply leaves mints it has no price for out of the response.
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "")
	prices, err := client.Prices(context.Background(), []string{"unknown-mint"})

	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if _, ok := prices["unknown-mint"]; ok {
		t.Fatal("expected no entry for a mint Jupiter doesn't price")
	}
}

func TestHTTPPriceClient_Prices_emptyMints_skipsRequest(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "")
	prices, err := client.Prices(context.Background(), nil)

	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if len(prices) != 0 {
		t.Fatalf("prices len = %d, want 0", len(prices))
	}
	if called {
		t.Fatal("expected no HTTP call for an empty mint list")
	}
}

func TestHTTPPriceClient_Prices_batchesOverFiftyMints(t *testing.T) {
	t.Parallel()

	var requestCount int
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		batchSizes = append(batchSizes, len(ids))
		body := "{"
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += fmt.Sprintf(`"%s": {"usdPrice": 1.0, "priceChange24h": 0}`, id)
		}
		body += "}"
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	mints := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		mints = append(mints, fmt.Sprintf("mint-%d", i))
	}

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "")
	prices, err := client.Prices(context.Background(), mints)

	if err != nil {
		t.Fatalf("Prices: %v", err)
	}
	if requestCount != 3 {
		t.Fatalf("requestCount = %d, want 3 batches for 120 mints", requestCount)
	}
	if batchSizes[0] != 50 || batchSizes[1] != 50 || batchSizes[2] != 20 {
		t.Fatalf("batchSizes = %v, want [50 50 20]", batchSizes)
	}
	if len(prices) != 120 {
		t.Fatalf("prices len = %d, want 120", len(prices))
	}
}

func TestHTTPPriceClient_Prices_non200_returnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewHTTPPriceClientWithBaseURL(server.URL, server.Client(), "")
	if _, err := client.Prices(context.Background(), []string{"mint1"}); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}
