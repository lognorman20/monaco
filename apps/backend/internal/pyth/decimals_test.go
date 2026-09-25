package pyth

import (
	"math/big"
	"math/rand"
	"testing"
)

func TestCostBasisMarkPerUnitMicros_eightDecimals(t *testing.T) {
	t.Parallel()

	totalUSDC := int64(200_000_000_000)
	tokenAtomics := int64(1_000) * 100_000_000

	mark, err := CostBasisMarkPerUnitMicros(totalUSDC, tokenAtomics, 8, big.NewRat(1, 1), "stock")
	if err != nil {
		t.Fatalf("CostBasisMarkPerUnitMicros: %v", err)
	}
	if mark != 200_000_000 {
		t.Fatalf("mark = %d, want 200_000_000 ($200/share)", mark)
	}
}

func TestCostBasisMark_nineDecimals(t *testing.T) {
	t.Parallel()

	totalUSDC := int64(500_000_000)
	tokenAtomics := int64(1_000_000_000)

	mark, err := CostBasisMarkPerUnitMicros(totalUSDC, tokenAtomics, 9, big.NewRat(1, 1), "pre_ipo")
	if err != nil {
		t.Fatalf("CostBasisMarkPerUnitMicros: %v", err)
	}
	if mark != 500_000_000 {
		t.Fatalf("mark = %d, want 500_000_000 ($500/token)", mark)
	}
}

func TestTokenAtomicsToDecimalUnits_nineDecimals(t *testing.T) {
	t.Parallel()

	got, err := TokenAtomicsToDecimalUnits(1_500_000_000, 9)
	if err != nil {
		t.Fatalf("TokenAtomicsToDecimalUnits: %v", err)
	}
	if got != "1.5" {
		t.Fatalf("units = %q, want 1.5", got)
	}
}

func TestMarkedPot_spacexPreStocks_multiplier5_valuesFiveTimesRaw(t *testing.T) {
	t.Parallel()

	const (
		raw      = int64(16_688_071)
		decimals = 9
		mark     = int64(116_740_000) // $116.74 per scaled unit
	)
	mult := big.NewRat(5, 1)

	oldValue, err := HoldingValueUSDCMicros(raw, mark, decimals, big.NewRat(1, 1), "pre_ipo")
	if err != nil {
		t.Fatal(err)
	}
	scaledValue, err := HoldingValueUSDCMicros(raw, mark, decimals, mult, "pre_ipo")
	if err != nil {
		t.Fatal(err)
	}
	diff := scaledValue - oldValue*5
	if diff < 0 {
		diff = -diff
	}
	if diff > 2 {
		t.Fatalf("scaled value = %d, want 5× raw-priced value %d (±2 micros)", scaledValue, oldValue*5)
	}
}

func TestProperty_potValueInvariantToMultiplier(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		raw := int64(rng.Intn(1_000_000_000)) + 1
		mark := int64(rng.Intn(900_000_000)) + 1_000_000
		multInt := int64(rng.Intn(8)+1) * 2
		k := int64(2)
		mult := big.NewRat(multInt, 1)

		left, err := HoldingValueUSDCMicros(raw, mark, 9, mult, "pre_ipo")
		if err != nil {
			t.Fatalf("left: %v", err)
		}
		right, err := HoldingValueUSDCMicros(raw*k, mark, 9, big.NewRat(multInt/k, 1), "pre_ipo")
		if err != nil {
			t.Fatalf("right: %v", err)
		}
		diff := left - right
		if diff < 0 {
			diff = -diff
		}
		if diff > 1 {
			t.Fatalf("raw=%d mult=%d mark=%d: left=%d right=%d diff=%d", raw, k, mark, left, right, diff)
		}
	}
}

func TestSellProbe_scaledUnit_roundsUp(t *testing.T) {
	t.Parallel()

	openAI := big.NewRat(14861347, 10_000_000)
	raw, err := RawAtomicsForOneScaledUnit(9, openAI)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(672_886_516)
	if raw != want {
		t.Fatalf("raw = %d, want %d", raw, want)
	}
}
