package httpapi

import (
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestQuotePriceUsdcMicros_usesXStockEightDecimals(t *testing.T) {
	t.Parallel()

	price, ok := quotePriceUsdcMicros(150_000, "44767", jupiter.XStockDecimals, nil, xstocks.AssetKindStock)
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	want := int64(335_068_242)
	if price != want {
		t.Fatalf("priceUsdcMicros = %d, want %d", price, want)
	}
}

func TestQuotePriceUsdcMicros_fixtureFiveUsdcTwoPointFiveShares(t *testing.T) {
	t.Parallel()

	price, ok := quotePriceUsdcMicros(5_000_000, "2500000", jupiter.XStockDecimals, nil, xstocks.AssetKindStock)
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	want := (5_000_000 * jupiter.XStockAtomicScale) / 2_500_000
	if price != want {
		t.Fatalf("priceUsdcMicros = %d, want %d ($%.2f/share)", price, want, float64(want)/1_000_000)
	}
}

func TestQuotePriceUsdcMicros_rejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	if _, ok := quotePriceUsdcMicros(1_000_000, "", jupiter.XStockDecimals, nil, xstocks.AssetKindStock); ok {
		t.Fatal("expected false for empty output")
	}
	if _, ok := quotePriceUsdcMicros(1_000_000, "0", jupiter.XStockDecimals, nil, xstocks.AssetKindStock); ok {
		t.Fatal("expected false for zero output")
	}
	if _, ok := quotePriceUsdcMicros(0, "1000", jupiter.XStockDecimals, nil, xstocks.AssetKindStock); ok {
		t.Fatal("expected false for zero usdc")
	}
}

func TestQuotePrice_nineDecimalOutAmount(t *testing.T) {
	t.Parallel()

	price, ok := quotePriceUsdcMicros(5_000_000, "2500000000", 9, nil, xstocks.AssetKindPreIPO)
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	want := (5_000_000 * jupiter.AtomicScale(9)) / 2_500_000_000
	if price != want {
		t.Fatalf("priceUsdcMicros = %d, want %d", price, want)
	}
}

func TestQuotePrice_scaledUnit(t *testing.T) {
	t.Parallel()

	// Measured: $10 USDC → 16_688_071 raw SPACEX at multiplier 5 → ~$119.85 per scaled unit.
	price, ok := quotePriceUsdcMicros(10_000_000, "16688071", 9, big.NewRat(5, 1), xstocks.AssetKindPreIPO)
	if !ok {
		t.Fatal("expected price derivation to succeed")
	}
	const wantApprox = 119_850_000 // USDC micros per scaled unit
	diff := price - wantApprox
	if diff < 0 {
		diff = -diff
	}
	if diff > 200_000 {
		t.Fatalf("priceUsdcMicros = %d, want about %d (±$0.20)", price, wantApprox)
	}
}
