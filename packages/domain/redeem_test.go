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
