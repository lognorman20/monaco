package domain

import "testing"

func TestParseUSDDecimal(t *testing.T) {
	valid := map[string]int64{
		"10.50":    10_500_000,
		"10":       10_000_000,
		"0.000001": 1,
		".5":       500_000,
		"007.25":   7_250_000,
	}
	for in, want := range valid {
		got, err := ParseUSDDecimal(in)
		if err != nil || got != want {
			t.Errorf("ParseUSDDecimal(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", "1.", "0", "0.000000", "-1", "+1", "1e3", "1.0000001", "1,5", " 1", "abc", "1.2.3", "NaN", "9223372036854.775808"} {
		if got, err := ParseUSDDecimal(in); err == nil {
			t.Errorf("ParseUSDDecimal(%q) = %d, want an error", in, got)
		}
	}
}

func TestParseTokenDecimal_scalesByTokenDecimals(t *testing.T) {
	got, err := ParseTokenDecimal("0.25", 8)
	if err != nil || got != 25_000_000 {
		t.Fatalf("ParseTokenDecimal(0.25, 8) = %d, %v; want 25_000_000", got, err)
	}
	if got, err := ParseTokenDecimal("0.000000001", 8); err == nil {
		t.Fatalf("nine decimals on an 8-decimal token = %d, want an error", got)
	}
	got, err = ParseTokenDecimal("92233720368.54775807", 8)
	if err != nil || got != 9_223_372_036_854_775_807 {
		t.Fatalf("max int64 in shares = %d, %v", got, err)
	}
	if got, err := ParseTokenDecimal("92233720368.54775808", 8); err == nil {
		t.Fatalf("one atomic past int64 = %d, want an error", got)
	}
}
