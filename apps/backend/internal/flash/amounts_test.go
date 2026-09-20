package flash

import "testing"

func TestAtomicsToDecimal_rendersNormalizedUnits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		atomics  int64
		decimals int
		want     string
	}{
		{5_000_000, 6, "5"},
		{1_500_000, 6, "1.5"},
		{1, 6, "0.000001"},
		{1_000_000, 8, "0.01"},
		{123_456_789_012, 8, "1234.56789012"},
		{42, 0, "42"},
	}
	for _, tc := range cases {
		got, err := AtomicsToDecimal(tc.atomics, tc.decimals)
		if err != nil {
			t.Fatalf("AtomicsToDecimal(%d, %d) error = %v", tc.atomics, tc.decimals, err)
		}
		if got != tc.want {
			t.Fatalf("AtomicsToDecimal(%d, %d) = %q, want %q", tc.atomics, tc.decimals, got, tc.want)
		}
	}
}

func TestAtomicsToDecimal_rejectsNonPositive(t *testing.T) {
	t.Parallel()

	for _, atomics := range []int64{0, -1} {
		if _, err := AtomicsToDecimal(atomics, 6); err == nil {
			t.Fatalf("AtomicsToDecimal(%d) expected error", atomics)
		}
	}
}

func TestDecimalToAtomics_parsesAndFloorsExcessPrecision(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw      string
		decimals int
		want     int64
	}{
		{"5", 6, 5_000_000},
		{"0.01486083", 8, 1_486_083},
		{"3.351309", 6, 3_351_309},
		{"3.3513099", 6, 3_351_309},
		{".5", 6, 500_000},
		{"0", 6, 0},
		{"0.0000001", 6, 0},
	}
	for _, tc := range cases {
		got, err := DecimalToAtomics(tc.raw, tc.decimals)
		if err != nil {
			t.Fatalf("DecimalToAtomics(%q, %d) error = %v", tc.raw, tc.decimals, err)
		}
		if got != tc.want {
			t.Fatalf("DecimalToAtomics(%q, %d) = %d, want %d", tc.raw, tc.decimals, got, tc.want)
		}
	}
}

func TestDecimalToAtomics_rejectsMalformed(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", " ", "-1", "1e6", "1.2.3", "abc", "1,5", "99999999999999999999"} {
		if _, err := DecimalToAtomics(raw, 6); err == nil {
			t.Fatalf("DecimalToAtomics(%q) expected error", raw)
		}
	}
}
