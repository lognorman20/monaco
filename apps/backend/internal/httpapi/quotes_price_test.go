package httpapi

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func TestQuotePriceUsdcMicros_usesXStockEightDecimals(t *testing.T) {
	t.Parallel()

	// Live Jupiter 0.15 USDC → AAPLx (2026-09-17): inAmount=150000, outAmount=44767.
	price, ok := quotePriceUsdcMicros(150_000, "44767")
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	want := int64(335_068_242) // ~$335.07/share in USDC micros
	if price != want {
		t.Fatalf("priceUsdcMicros = %d, want %d", price, want)
	}
}

func TestQuotePriceUsdcMicros_fixtureFiveUsdcTwoPointFiveShares(t *testing.T) {
	t.Parallel()

	// 5 USDC buys 2.5 AAPLx shares when outAmount=2_500_000 (8 dp atomics).
	price, ok := quotePriceUsdcMicros(5_000_000, "2500000")
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	want := (5_000_000 * b20.TokenAtomicScale) / 2_500_000
	if price != want {
		t.Fatalf("priceUsdcMicros = %d, want %d ($%.2f/share)", price, want, float64(want)/1_000_000)
	}
}

func TestQuotePriceUsdcMicros_rejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	if _, ok := quotePriceUsdcMicros(1_000_000, ""); ok {
		t.Fatal("expected false for empty output")
	}
	if _, ok := quotePriceUsdcMicros(1_000_000, "0"); ok {
		t.Fatal("expected false for zero output")
	}
	if _, ok := quotePriceUsdcMicros(0, "1000"); ok {
		t.Fatal("expected false for zero usdc")
	}
}
