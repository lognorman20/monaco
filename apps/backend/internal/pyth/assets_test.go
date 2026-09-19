package pyth

import "testing"

func TestParseChartRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want ChartRange
		ok   bool
	}{
		{raw: "", want: ChartRange1D, ok: true},
		{raw: "1D", want: ChartRange1D, ok: true},
		{raw: "1w", want: ChartRange1W, ok: true},
		{raw: "1M", want: ChartRange1M, ok: true},
		{raw: "1Y", ok: false},
	}
	for _, tc := range cases {
		got, err := ParseChartRange(tc.raw)
		if tc.ok {
			if err != nil {
				t.Fatalf("ParseChartRange(%q) err = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParseChartRange(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("ParseChartRange(%q) err = nil, want error", tc.raw)
		}
	}
}

func TestMidSpreadBps(t *testing.T) {
	t.Parallel()
	bps := MidSpreadBps(185_000_000, "100000000", 1_000_000, 8)
	if bps == nil {
		t.Fatal("expected spread bps")
	}
	if *bps == 0 {
		t.Fatalf("spread bps = 0, want non-zero from 1 USDC / 1 share vs $185 mark")
	}
	if MidSpreadBps(0, "100000000", 1_000_000, 8) != nil {
		t.Fatal("zero mark should return nil spread")
	}
}
