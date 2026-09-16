package jupiter

import (
	"errors"
	"testing"
)

func TestJupiterQuoteBuy_validRoute_returnsQuoteWithUsdcInputMint(t *testing.T) {
	t.Parallel()

	// Arrange
	const outputMint = "XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp"

	// Act
	quote, err := ParseBuyQuoteResponse(FixtureJupiterSuccessResponse(outputMint))

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
