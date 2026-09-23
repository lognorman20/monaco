package jupiter

import "testing"

func TestRedeemShortfallSellAmount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		holding    int64
		shortfall  int64
		stockValue int64
		want       int64
	}{
		{
			name:       "sells only the shortfall plus a slippage buffer",
			holding:    2_000_000,
			shortfall:  500_000,
			stockValue: 2_000_000,
			want:       505_001,
		},
		{
			name:       "shortfall at or above the whole book sells everything",
			holding:    2_000_000,
			shortfall:  2_000_000,
			stockValue: 2_000_000,
			want:       2_000_000,
		},
		{
			name:       "no shortfall sells nothing",
			holding:    2_000_000,
			shortfall:  0,
			stockValue: 2_000_000,
			want:       0,
		},
		{
			name:       "negative shortfall sells nothing",
			holding:    2_000_000,
			shortfall:  -10,
			stockValue: 2_000_000,
			want:       0,
		},
		{
			name:       "unmarked stock sells nothing",
			holding:    2_000_000,
			shortfall:  500_000,
			stockValue: 0,
			want:       0,
		},
		{
			name:       "large holdings do not overflow",
			holding:    900_000_000_000_000,
			shortfall:  1_000_000_000_000,
			stockValue: 4_000_000_000_000,
			want:       227_250_000_000_225,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RedeemShortfallSellAmount(tc.holding, tc.shortfall, tc.stockValue, RedeemSellSlippageBufferBps)
			if got != tc.want {
				t.Fatalf("RedeemShortfallSellAmount(%d, %d, %d, %d) = %d, want %d",
					tc.holding, tc.shortfall, tc.stockValue, RedeemSellSlippageBufferBps, got, tc.want)
			}
			if got > tc.holding {
				t.Fatalf("sell amount %d exceeds holding %d", got, tc.holding)
			}
		})
	}
}

func TestRedeemShortfallSellAmount_feeAlreadyInQuote_staysAt100Bps(t *testing.T) {
	t.Parallel()

	const (
		holding    = 2_000_000
		shortfall  = 500_000
		stockValue = 2_000_000
	)

	got := RedeemShortfallSellAmount(holding, shortfall, stockValue, RedeemSellSlippageBufferBps)
	if got != 505_001 {
		t.Fatalf("with slippage-only buffer got %d, want 505_001", got)
	}

	withFee := RedeemShortfallSellAmount(holding, shortfall, stockValue, RedeemSellSlippageBufferBps+20)
	if withFee <= got {
		t.Fatalf("120 bps buffer should sell more than 100 bps: got %d vs %d", withFee, got)
	}
}

func TestRedeemShortfallSellAmount_feeNotInQuote_uses120Bps(t *testing.T) {
	t.Parallel()

	const (
		holding    = 2_000_000
		shortfall  = 500_000
		stockValue = 2_000_000
		bufferBps  = RedeemSellSlippageBufferBps + 20
	)

	got := RedeemShortfallSellAmount(holding, shortfall, stockValue, bufferBps)
	if got != 506_001 {
		t.Fatalf("RedeemShortfallSellAmount(..., %d) = %d, want 506_001", bufferBps, got)
	}
	if got > holding {
		t.Fatalf("sell amount %d exceeds holding %d", got, holding)
	}
}

func TestRedeemShortfallSellAmount_nineDecimalHolding_roundsUpAtomics(t *testing.T) {
	t.Parallel()

	const (
		holding    = 3_000_000_000
		shortfall  = 250_000_000
		stockValue = 4_000_000_000
	)

	got := RedeemShortfallSellAmount(holding, shortfall, stockValue, RedeemSellSlippageBufferBps)
	if got != 189_375_001 {
		t.Fatalf("nine-decimal holding rounded up to %d, want 189_375_001", got)
	}
	if got > holding {
		t.Fatalf("sell amount %d exceeds holding %d", got, holding)
	}
}
