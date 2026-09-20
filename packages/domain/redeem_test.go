package domain

import "testing"

func TestComputeRedeemSlice_proportionalToShares(t *testing.T) {
	slice, err := ComputeRedeemSlice(RedeemSliceInput{
		SharesRedeemedMicros: 500_000,
		TotalSharesMicros:    2_000_000,
		PotNav:               4_000_000,
	})
	if err != nil {
		t.Fatalf("ComputeRedeemSlice: %v", err)
	}
	if slice.UsdcOwed != 1_000_000 {
		t.Fatalf("UsdcOwed = %d, want 1000000", slice.UsdcOwed)
	}
}

func TestComputeRedeemSlice_usdcOnlyPot_fullRedeem(t *testing.T) {
	slice, err := ComputeRedeemSlice(RedeemSliceInput{
		SharesRedeemedMicros: 3_000_000,
		TotalSharesMicros:    3_000_000,
		PotNav:               3_000_000,
	})
	if err != nil {
		t.Fatalf("ComputeRedeemSlice: %v", err)
	}
	if slice.UsdcOwed != 3_000_000 {
		t.Fatalf("UsdcOwed = %d, want 3000000", slice.UsdcOwed)
	}
}

func TestShareUnitsForPartialPayout(t *testing.T) {
	cases := []struct {
		name       string
		shareUnits int64
		owed, paid USDCMicros
		want       int64
	}{
		{"paid in full burns every share", 500_000, 500_000, 500_000, 500_000},
		{"overpaid never burns more than the job holds", 500_000, 500_000, 600_000, 500_000},
		{"short payout burns the paid fraction", 500_000, 500_000, 480_000, 480_000},
		{"fraction rounds up against the redeemer", 3, 1_000_000, 333_334, 2},
		{"smallest payout still burns a share", 1_000_000, 1_000_000_000_000, 1, 1},
		{"large values do not overflow", 9_000_000_000_000_000, 9_000_000_000_000_000, 4_500_000_000_000_000, 4_500_000_000_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ShareUnitsForPartialPayout(tc.shareUnits, tc.owed, tc.paid)
			if err != nil {
				t.Fatalf("ShareUnitsForPartialPayout: %v", err)
			}
			if got != tc.want {
				t.Fatalf("burned = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestShareUnitsForPartialPayout_rejectsNonPositiveInputs(t *testing.T) {
	for _, tc := range []struct {
		shareUnits int64
		owed, paid USDCMicros
	}{{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1}} {
		if _, err := ShareUnitsForPartialPayout(tc.shareUnits, tc.owed, tc.paid); err == nil {
			t.Fatalf("ShareUnitsForPartialPayout(%d, %d, %d): want error", tc.shareUnits, tc.owed, tc.paid)
		}
	}
}
