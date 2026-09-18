package main

import "testing"

func TestFormatSOL(t *testing.T) {
	tests := []struct {
		lamports uint64
		want     string
	}{
		{0, "0.000000000"},
		{3_044_217, "0.003044217"},
		{1_000_000_000, "1.000000000"},
	}
	for _, tc := range tests {
		if got := formatSOL(tc.lamports); got != tc.want {
			t.Fatalf("formatSOL(%d) = %q, want %q", tc.lamports, got, tc.want)
		}
	}
}

func TestFormatUSDC(t *testing.T) {
	tests := []struct {
		micros uint64
		want   string
	}{
		{0, "0.00"},
		{1_500_000, "1.50"},
		{44_765, "0.04"},
	}
	for _, tc := range tests {
		if got := formatUSDC(tc.micros); got != tc.want {
			t.Fatalf("formatUSDC(%d) = %q, want %q", tc.micros, got, tc.want)
		}
	}
}
