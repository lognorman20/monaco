package pyth

import "testing"

func TestEquityQuerySymbol_mapsXStockToHermesEquityFeed(t *testing.T) {
	query := EquityQuerySymbol("AAPLx")
	if query != "Equity.US.AAPL/USD" {
		t.Fatalf("query = %q, want Equity.US.AAPL/USD", query)
	}
}

func TestEquityQuerySymbol_stripsTrailingC(t *testing.T) {
	if got := EquityQuerySymbol("AAPLc"); got != "Equity.US.AAPL/USD" {
		t.Fatalf("got %q", got)
	}
}

func TestEquityQuerySymbol_stripsOnlyTheLowercaseTokenSuffix(t *testing.T) {
	// SPCX and NFLX end in an upper-case X that belongs to the ticker. Upper-casing
	// before trimming turned SPCXc into Equity.US.SPC/USD — another company's feed.
	cases := map[string]string{
		"INTCc":   "Equity.US.INTC/USD",
		"SPCXc":   "Equity.US.SPCX/USD",
		"NFLXc":   "Equity.US.NFLX/USD",
		"TSLAc":   "Equity.US.TSLA/USD",
		" MSFTc ": "Equity.US.MSFT/USD",
		"NFLXx":   "Equity.US.NFLX/USD",
		"SPCX":    "Equity.US.SPCX/USD",
		"AAPLC":   "Equity.US.AAPLC/USD",
	}
	for symbol, want := range cases {
		if got := EquityQuerySymbol(symbol); got != want {
			t.Errorf("EquityQuerySymbol(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestUnderlyingTicker_stripsExactlyOneSuffix(t *testing.T) {
	cases := map[string]string{
		"AAPLc": "AAPL",
		"SPCXc": "SPCX",
		"COINc": "COIN",
		"CRCLc": "CRCL",
		"AAPLx": "AAPL",
		"ABCcc": "ABCC",
	}
	for symbol, want := range cases {
		if got := UnderlyingTicker(symbol); got != want {
			t.Errorf("UnderlyingTicker(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestFeedRegistry_isKeyedByQueryString(t *testing.T) {
	ClearFeedRegistry()
	t.Cleanup(ClearFeedRegistry)

	RegisterFeedID("AAPLc", "feed-aapl")
	if id, ok := lookupFeedID("Equity.US.AAPL/USD"); !ok || id != "feed-aapl" {
		t.Fatalf("lookup = %q, %v", id, ok)
	}
	// The legacy symbol names the same equity, so it shares the feed.
	if id, ok := lookupFeedID(EquityQuerySymbol("AAPLx")); !ok || id != "feed-aapl" {
		t.Fatalf("legacy lookup = %q, %v", id, ok)
	}
	if _, ok := lookupFeedID(EquityQuerySymbol("SPCXc")); ok {
		t.Fatal("an unregistered equity must not resolve")
	}
}
