package flash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testUSDCMint  = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	testAAPLxMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
	testFunder    = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	testAPIKey    = "test-flash-key"
)

func testBuyQuoteParams() QuoteParams {
	return QuoteParams{
		GroupID:       "group-1",
		UserID:        "user-1",
		Symbol:        "AAPLx",
		Side:          "buy",
		TargetAsset:   testAAPLxMint,
		ContraAsset:   testUSDCMint,
		Qty:           "5",
		MaxSlippage:   "0.01",
		FunderAddress: testFunder,
	}
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *HTTPClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewHTTPClientWithBaseURL(server.URL, testAPIKey, server.Client())
}

func TestFlashQuote_sendsApiKeyAndSolanaMarketBody(t *testing.T) {
	t.Parallel()

	// Arrange
	var gotKey, gotPath string
	var gotBody map[string]any
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-definitive-api-key")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write(FixtureQuoteReady("buy", testAAPLxMint, testUSDCMint, "DFS|m="+testUSDCMint+"|t=5000000|h=abc", "1789861368"))
	})

	// Act
	quote, err := client.Quote(context.Background(), testBuyQuoteParams())

	// Assert
	if err != nil {
		t.Fatalf("Quote() error = %v", err)
	}
	if gotKey != testAPIKey {
		t.Fatalf("api key header = %q, want %q", gotKey, testAPIKey)
	}
	if gotPath != "/quote" {
		t.Fatalf("path = %q, want /quote", gotPath)
	}
	want := map[string]any{
		"targetChain": "solana", "contraChain": "solana", "orderType": "market",
		"side": "buy", "qty": "5", "maxSlippage": "0.01",
		"targetAsset": testAAPLxMint, "contraAsset": testUSDCMint, "funderAddress": testFunder,
	}
	for key, value := range want {
		if gotBody[key] != value {
			t.Fatalf("body[%s] = %v, want %v", key, gotBody[key], value)
		}
	}
	if quote.QuoteID != "quote-ready" || quote.Nonce != "932385860354111" || quote.Deadline != "1789861368" {
		t.Fatalf("unexpected quote %+v", quote)
	}
	if quote.NeedsOnchainSetup() {
		t.Fatal("expected no onchain setup for a ready funder")
	}
}

func TestFlashQuote_firstTrade_returnsSetupInstructions(t *testing.T) {
	t.Parallel()

	// Act
	quote, err := ParseQuoteResponse(FixtureQuoteNeedsSetup(testFunder, testAAPLxMint, testUSDCMint))

	// Assert
	if err != nil {
		t.Fatalf("ParseQuoteResponse() error = %v", err)
	}
	if !quote.NeedsOnchainSetup() {
		t.Fatal("expected onchain setup")
	}
	if len(quote.ATASetupIxs) != 1 || len(quote.ATASetupIxs[0].Accounts) != 6 {
		t.Fatalf("unexpected ata setup %+v", quote.ATASetupIxs)
	}
	if quote.DelegateIx == nil || quote.DelegateIx.Data != "4h6bzpF8MKT4" {
		t.Fatalf("unexpected delegate ix %+v", quote.DelegateIx)
	}
}

func TestFlashQuote_missingApiKey_failsBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	// Arrange
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	t.Cleanup(server.Close)
	client := NewHTTPClientWithBaseURL(server.URL, "  ", server.Client())

	// Act
	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	// Assert
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Quote() error = %v, want ErrUnauthorized", err)
	}
	if called {
		t.Fatal("expected no HTTP request without an API key")
	}
}

func TestFlashQuote_unauthorized_returnsErrUnauthorized(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(FixtureUnauthorized())
	})

	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Quote() error = %v, want ErrUnauthorized", err)
	}
	if !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("error = %v, want flat error message surfaced", err)
	}
}

func TestFlashQuote_unknownAsset_returnsErrNoRoute(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(FixtureQuoteUnknownAsset(testAAPLxMint))
	})

	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("Quote() error = %v, want ErrNoRoute", err)
	}
}

func TestFlashQuote_serverError_returnsAPIErrorNotNoRoute(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`<html>upstream unavailable</html>`))
	})

	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("Quote() error = %v, want APIError 503", err)
	}
	if errors.Is(err, ErrNoRoute) {
		t.Fatal("an outage must not read as no route")
	}
}

func TestFlashQuote_malformedJSON_returnsError(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"quoteId": "q-1", "svm": [`))
	})

	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	if err == nil || !strings.Contains(err.Error(), "invalid quote json") {
		t.Fatalf("Quote() error = %v, want invalid quote json", err)
	}
}

func TestFlashQuote_missingQuoteID_returnsError(t *testing.T) {
	t.Parallel()

	_, err := ParseQuoteResponse([]byte(`{"from":{"amount":"5"},"svm":null}`))

	if err == nil || !strings.Contains(err.Error(), "missing quoteId") {
		t.Fatalf("ParseQuoteResponse() error = %v, want missing quoteId", err)
	}
}

func TestFlashSubmitOrder_echoesQuoteSigningFields(t *testing.T) {
	t.Parallel()

	// Arrange
	var gotBody map[string]any
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"orderId":"ord-1"}`))
	})

	// Act
	orderID, err := client.SubmitOrder(context.Background(), SubmitOrderParams{
		Quote:         testBuyQuoteParams(),
		QuoteID:       "quote-ready",
		UserSignature: "sig-base58",
		Nonce:         "932385860354111",
		Deadline:      "1789861368",
	})

	// Assert
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if orderID != "ord-1" {
		t.Fatalf("orderID = %q, want ord-1", orderID)
	}
	want := map[string]any{
		"quoteId": "quote-ready", "userSignature": "sig-base58",
		"svmNonce": "932385860354111", "svmDeadline": "1789861368",
		"funderAddress": testFunder, "qty": "5", "side": "buy",
	}
	for key, value := range want {
		if gotBody[key] != value {
			t.Fatalf("body[%s] = %v, want %v", key, gotBody[key], value)
		}
	}
	for _, evmOnly := range []string{"evmOrderTypedData", "evmPermitTypedData", "evmPermitSignature", "svmSponsoredDelegateTx"} {
		if _, present := gotBody[evmOnly]; present {
			t.Fatalf("body must omit %s on an unsponsored Solana order", evmOnly)
		}
	}
}

func TestFlashSubmitOrder_rejected_returnsAPIError(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"FAILED_PRECONDITION","message":"delegation does not cover order total"}}`))
	})

	_, err := client.SubmitOrder(context.Background(), SubmitOrderParams{
		Quote: testBuyQuoteParams(), QuoteID: "q", UserSignature: "s", Nonce: "1", Deadline: "2",
	})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "FAILED_PRECONDITION" {
		t.Fatalf("SubmitOrder() error = %v, want FAILED_PRECONDITION APIError", err)
	}
}

func TestFlashSubmitOrder_missingOrderID_returnsError(t *testing.T) {
	t.Parallel()

	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})

	_, err := client.SubmitOrder(context.Background(), SubmitOrderParams{
		Quote: testBuyQuoteParams(), QuoteID: "q", UserSignature: "s", Nonce: "1", Deadline: "2",
	})

	if err == nil || !strings.Contains(err.Error(), "missing orderId") {
		t.Fatalf("SubmitOrder() error = %v, want missing orderId", err)
	}
}

func TestFlashSubmitOrder_missingSignature_failsBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	called := false
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })

	_, err := client.SubmitOrder(context.Background(), SubmitOrderParams{Quote: testBuyQuoteParams(), QuoteID: "q"})

	if err == nil || called {
		t.Fatalf("SubmitOrder() error = %v called = %v, want local validation error", err, called)
	}
}

func TestFlashGetOrder_filled_readsTotalsAndSignature(t *testing.T) {
	t.Parallel()

	// Arrange
	var gotPath, gotFunder string
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotFunder = r.URL.Query().Get("funderAddress")
		_, _ = w.Write(FixtureOrderFilled("ord-1", "flash-sig"))
	})

	// Act
	order, err := client.GetOrder(context.Background(), GetOrderParams{OrderID: "ord-1", FunderAddress: testFunder})

	// Assert
	if err != nil {
		t.Fatalf("GetOrder() error = %v", err)
	}
	if gotPath != "/orders/ord-1" || gotFunder != testFunder {
		t.Fatalf("path = %q funder = %q", gotPath, gotFunder)
	}
	if !order.IsFilled() || order.TransactionID != "flash-sig" {
		t.Fatalf("unexpected order %+v", order)
	}
	if order.FilledTargetAmount != "0.01486083" || order.FilledContraAmount != "5" {
		t.Fatalf("unexpected filled totals %+v", order)
	}
}

func TestFlashGetOrder_malformedOrMissingStatus_returnsError(t *testing.T) {
	t.Parallel()

	if _, err := ParseOrderResponse([]byte(`not json`)); err == nil {
		t.Fatal("expected invalid json error")
	}
	if _, err := ParseOrderResponse([]byte(`{"order":{"orderId":"ord-1"},"fills":[]}`)); err == nil {
		t.Fatal("expected missing status error")
	}
}

func TestFlashHTTPClient_liveHostBlockedDuringGoTest(t *testing.T) {
	t.Parallel()

	client := NewHTTPClient(testAPIKey)

	_, err := client.Quote(context.Background(), testBuyQuoteParams())

	if err == nil || !strings.Contains(err.Error(), "live API HTTP blocked") {
		t.Fatalf("Quote() error = %v, want live API block", err)
	}
}
