package yahoocharts

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

func TestUnderlyingTicker(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"AAPLx": "AAPL", "BRK.Bx": "BRK-B", "SPYx": "SPY", " NVDAx ": "NVDA",
		// Not xStocks: a bare ticker, the stablecoin, and every pre-IPO issuer's spelling.
		"AAPL": "", "USDC": "", "": "", "x": "", "aaplx": "",
		"tKalshi": "", "tSpaceX": "", "tOpenAI": "", "ANDURIL": "", "POLYMARKET": "",
	}
	for in, want := range cases {
		if got := UnderlyingTicker(in); got != want {
			t.Errorf("UnderlyingTicker(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeBars_skipsNullClosesAndFillsGaps(t *testing.T) {
	t.Parallel()
	body := []byte(`{"chart":{"result":[{"timestamp":[1700000000,1700000300,1700000600],
		"indicators":{"quote":[{"open":[100.0,null,102.5],"high":[101.0,null,103.0],"low":[99.5,null,102.0],"close":[100.5,null,102.75]}]}}],"error":null}}`)
	bars, err := DecodeBars(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 2 {
		t.Fatalf("bars = %d, want 2 (the null close is skipped)", len(bars))
	}
	if bars[0].Close != 100_500_000 || bars[0].Open != 100_000_000 || bars[1].Timestamp != 1700000600 {
		t.Fatalf("unexpected bars: %+v", bars)
	}
	if _, err := DecodeBars([]byte(`{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found"}}}`)); err == nil {
		t.Fatal("an upstream error must surface")
	}
}

func TestSeries_asksForTheEquityAndLabelsTheBasis(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 18, 30, 0, 0, time.UTC)
	// Six five-minute closes inside that day's cash session, plus one from the day
	// before so the previous close is in the payload.
	session := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)
	stamps := []int64{time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC).Unix()}
	for i := 0; i < 6; i++ {
		stamps = append(stamps, session.Add(time.Duration(i)*5*time.Minute).Unix())
	}
	closes := "230.0,231.0,231.4,231.2,231.9,232.1,232.05"
	payload := fmt.Sprintf(`{"chart":{"result":[{"timestamp":[%d,%d,%d,%d,%d,%d,%d],"indicators":{"quote":[{"close":[%s],"open":[%s],"high":[%s],"low":[%s]}]}}],"error":null}}`,
		stamps[0], stamps[1], stamps[2], stamps[3], stamps[4], stamps[5], stamps[6], closes, closes, closes, closes)
	var gotPath, gotQuery, gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		w.Write([]byte(payload))
	}))
	defer server.Close()
	client := NewWithBaseURL(server.URL, server.Client())
	series, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, now)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v8/finance/chart/AAPL" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery == "" || gotUA == "" {
		t.Fatalf("query %q or user agent %q missing", gotQuery, gotUA)
	}
	if series.Source != pyth.ChartSourceYahoo || series.Basis != pyth.PriceBasisUnderlying || series.BasisSymbol != "AAPL" {
		t.Fatalf("series not labelled: source=%q basis=%q symbol=%q", series.Source, series.Basis, series.BasisSymbol)
	}
	if len(series.Points) == 0 {
		t.Fatalf("no points assembled: %+v", series)
	}
	if !client.HasKeylessHistory() {
		t.Fatal("yahoo needs no key")
	}
}
