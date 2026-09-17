package jupiter

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint(t *testing.T) {
	t.Parallel()

	// Arrange
	const outputMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"

	// Act
	quote, err := ParseBuyQuoteResponse(FixtureJupiterSuccessResponse(outputMint), false)

	// Assert
	if err != nil {
		t.Fatalf("ParseBuyQuoteResponse() error = %v", err)
	}
	if !quote.Routable {
		t.Fatal("expected routable quote")
	}
	if quote.InputMint != USDCMint {
		t.Fatalf("InputMint = %q, want %q", quote.InputMint, USDCMint)
	}
	if quote.OutputMint != outputMint {
		t.Fatalf("OutputMint = %q, want %q", quote.OutputMint, outputMint)
	}
	if quote.InAmount != "1000000" {
		t.Fatalf("InAmount = %q, want %q", quote.InAmount, "1000000")
	}
	if quote.OutAmount != "500000" {
		t.Fatalf("OutAmount = %q, want %q", quote.OutAmount, "500000")
	}
}

func TestJupiterQuoteBuy_noRoute_returnsRoutableFalse(t *testing.T) {
	t.Parallel()

	// Arrange
	const outputMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
	client := NewFakeClient()
	RegisterQuoteBuy(client, outputMint, 1_000_000, BuyQuote{
		Routable:   false,
		InputMint:  USDCMint,
		OutputMint: outputMint,
	})

	// Act
	quote, err := client.QuoteBuy(t.Context(), QuoteBuyParams{
		GroupID:    "group-1",
		UserID:     "user-1",
		Symbol:     "AAPLx",
		OutputMint: outputMint,
		USDCAmount: 1_000_000,
	})

	// Assert
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("QuoteBuy() error = %v, want ErrNoRoute", err)
	}
	if quote.Routable {
		t.Fatal("expected Routable=false")
	}
	if quote.InputMint != USDCMint {
		t.Fatalf("InputMint = %q, want %q", quote.InputMint, USDCMint)
	}
}

func TestJupiterQuoteBuy_rfqWithoutTransaction_notRoutableWhenTakerRequired(t *testing.T) {
	t.Parallel()

	// Jupiter RFQ returns routePlan + outAmount but transaction=null without taker.
	body := []byte(`{
  "inputMint": "` + USDCMint + `",
  "outputMint": "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
  "inAmount": "150000",
  "outAmount": "44765",
  "transaction": null,
  "routePlan": [{"percent": 100, "bps": 10000, "swapInfo": {"inputMint": "` + USDCMint + `", "outputMint": "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp", "inAmount": "150000", "outAmount": "44765"}}],
  "requestId": "req-rfq-no-tx"
}`)

	quote, err := ParseBuyQuoteResponse(body, false)
	if err != nil {
		t.Fatalf("ParseBuyQuoteResponse(price-only) error = %v", err)
	}
	if !quote.Routable {
		t.Fatal("expected price-only RFQ quote to be routable")
	}

	_, err = ParseBuyQuoteResponse(body, true)
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("ParseBuyQuoteResponse(executable) error = %v, want ErrNoRoute", err)
	}
}

func TestFetchBuyOrder_withPayer_includesPayerQueryParam(t *testing.T) {
	t.Parallel()

	const (
		taker = "Treasury1111111111111111111111111111111111"
		payer = "Relayer11111111111111111111111111111111111"
	)
	var gotPayer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPayer = r.URL.Query().Get("payer")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(FixtureJupiterSuccessResponse("XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"))
	}))
	defer server.Close()

	client := NewHTTPClientWithBaseURL(server.URL, server.Client())
	client.payer = payer
	_, err := client.fetchBuyOrder(t.Context(), buyOrderRequest{
		OutputMint: "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp",
		Amount:     150_000,
		Taker:      taker,
	}, "group-1", "user-1", "AAPLx")
	if err != nil {
		t.Fatalf("fetchBuyOrder() error = %v", err)
	}
	if gotPayer != payer {
		t.Fatalf("payer query = %q, want %q", gotPayer, payer)
	}
}

func TestOrderBuildError_mapsInsufficientFunds(t *testing.T) {
	t.Parallel()

	err := orderBuildError("dflow", 1, "Insufficient funds", "Insufficient funds")
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("error = %v, want ErrInsufficientFunds", err)
	}
}

func TestJupiterQuoteBuy_httpError_propagatesAsRefusal(t *testing.T) {
	t.Parallel()

	// Arrange
	const outputMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClientWithBaseURL(server.URL, server.Client())

	// Act
	quote, err := client.QuoteBuy(t.Context(), QuoteBuyParams{
		GroupID:    "group-1",
		UserID:     "user-1",
		Symbol:     "AAPLx",
		OutputMint: outputMint,
		USDCAmount: 1_000_000,
	})

	// Assert
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("QuoteBuy() error = %v, want ErrNoRoute", err)
	}
	if quote.Routable {
		t.Fatal("expected Routable=false on HTTP error")
	}
	if quote.InputMint != USDCMint {
		t.Fatalf("InputMint = %q, want %q", quote.InputMint, USDCMint)
	}
	if quote.OutputMint != outputMint {
		t.Fatalf("OutputMint = %q, want %q", quote.OutputMint, outputMint)
	}
}
