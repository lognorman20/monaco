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
			got := RedeemShortfallSellAmount(tc.holding, tc.shortfall, tc.stockValue)
			if got != tc.want {
				t.Fatalf("RedeemShortfallSellAmount(%d, %d, %d) = %d, want %d",
					tc.holding, tc.shortfall, tc.stockValue, got, tc.want)
			}
			if got > tc.holding {
				t.Fatalf("sell amount %d exceeds holding %d", got, tc.holding)
			}
		})
	}
}
