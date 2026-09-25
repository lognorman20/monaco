package jupitercharts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// A Wednesday afternoon inside a regular US session, so the 1D window is a real
// trading day rather than a weekend fallback.
var chartsNow = time.Date(2026, 9, 23, 18, 0, 0, 0, time.UTC)

type stubMints struct {
	mint string
	err  error
}

func (s stubMints) ResolveSolanaMint(context.Context, string) (string, error) {
	return s.mint, s.err
}

// candlesJSON builds a response of `count` candles ending at `end`, stepping back
// by `step` and rising a cent each bar so a series is easy to assert on.
func candlesJSON(end time.Time, step time.Duration, count int, startUSD float64) string {
	type candle struct {
		Time  int64   `json:"time"`
		Open  float64 `json:"open"`
		High  float64 `json:"high"`
		Low   float64 `json:"low"`
		Close float64 `json:"close"`
	}
	candles := make([]candle, 0, count)
	for i := count - 1; i >= 0; i-- {
		price := startUSD + float64(count-1-i)*0.01
		candles = append(candles, candle{
			Time:  end.Add(-time.Duration(i) * step).Unix(),
			Open:  price,
			High:  price + 0.02,
			Low:   price - 0.02,
			Close: price,
		})
	}
	body, err := json.Marshal(map[string]any{"candles": candles})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func chartsServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestChartsClient_Series_buildsATokenSeriesFromCandles(t *testing.T) {
	var gotPath, gotInterval, gotType string
	server := chartsServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInterval = r.URL.Query().Get("interval")
		gotType = r.URL.Query().Get("type")
		fmt.Fprint(w, candlesJSON(chartsNow, 5*time.Minute, 200, 230.00))
	})

	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
	series, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, chartsNow)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}

	if gotPath != "/XsAAPL" {
		t.Errorf("path = %q, want /XsAAPL — the resolved mint is what Jupiter is asked about", gotPath)
	}
	if gotInterval != "5_MINUTE" {
		t.Errorf("interval = %q, want 5_MINUTE for 1D", gotInterval)
	}
	if gotType != "price" {
		t.Errorf("type = %q, want price", gotType)
	}
	if len(series.Points) == 0 {
		t.Fatal("no points; a 1D window inside a session must draw a curve")
	}
	if series.Source != pyth.ChartSourceJupiter {
		t.Errorf("source = %q, want %q", series.Source, pyth.ChartSourceJupiter)
	}
	// The whole reason for this source: the curve is the token a cabal buys, not the
	// equity behind it, and it has to say so.
	if series.Basis != pyth.PriceBasisToken {
		t.Errorf("basis = %q, want %q", series.Basis, pyth.PriceBasisToken)
	}
	if series.BasisSymbol != "AAPLx" {
		t.Errorf("basisSymbol = %q, want AAPLx", series.BasisSymbol)
	}
	for i, point := range series.Points {
		if point.PriceUsdcMicros <= 0 {
			t.Fatalf("point %d has a non-positive price: %d", i, point.PriceUsdcMicros)
		}
		if i > 0 && point.Timestamp <= series.Points[i-1].Timestamp {
			t.Fatalf("points are not strictly increasing in time at %d", i)
		}
	}
}

func TestChartsClient_Series_rangesPickTheirOwnInterval(t *testing.T) {
	for _, tc := range []struct {
		chartRange pyth.ChartRange
		want       string
	}{
		{pyth.ChartRange1D, "5_MINUTE"},
		{pyth.ChartRange1W, "30_MINUTE"},
		{pyth.ChartRange1M, "1_HOUR"},
		{pyth.ChartRange3M, "1_DAY"},
		{pyth.ChartRange1Y, "1_DAY"},
		{pyth.ChartRangeAll, "1_DAY"},
	} {
		t.Run(string(tc.chartRange), func(t *testing.T) {
			var got string
			var candles int
			server := chartsServer(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("interval")
				candles, _ = strconv.Atoi(r.URL.Query().Get("candles"))
				fmt.Fprint(w, `{"candles":[]}`)
			})
			client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
			if _, err := client.Series(context.Background(), "AAPLx", tc.chartRange, chartsNow); err != nil {
				t.Fatalf("Series: %v", err)
			}
			if got != tc.want {
				t.Errorf("interval = %q, want %q", got, tc.want)
			}
			if candles <= 0 || candles > maxCandlesPerRequest {
				t.Errorf("candles = %d, want between 1 and %d", candles, maxCandlesPerRequest)
			}
		})
	}
}

// Jupiter counts candles backwards from `to`, so asking for too few would silently
// drop the oldest end of the window — including the bar the previous close is taken
// from, which is the baseline the day change is measured against.
func TestChartsClient_Series_asksForEnoughCandlesToCoverTheWindow(t *testing.T) {
	var asked int
	server := chartsServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked, _ = strconv.Atoi(r.URL.Query().Get("candles"))
		fmt.Fprint(w, `{"candles":[]}`)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
	if _, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1M, chartsNow); err != nil {
		t.Fatalf("Series: %v", err)
	}

	from, to := pyth.RangeFetchWindow(pyth.ChartRange1M, chartsNow)
	need := int(to.Sub(from).Hours()) // 1M uses hourly candles
	if asked < need {
		t.Errorf("asked for %d candles but the fetch window spans %d hours", asked, need)
	}
}

func TestChartsClient_Series_sendsTheApiKeyWhenThereIsOne(t *testing.T) {
	var got string
	server := chartsServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-api-key")
		fmt.Fprint(w, `{"candles":[]}`)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "sk-test")
	if _, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, chartsNow); err != nil {
		t.Fatalf("Series: %v", err)
	}
	if got != "sk-test" {
		t.Errorf("x-api-key = %q, want sk-test", got)
	}
}

// A mint with no candles is an answer, not an outage: the caller has to be able to
// cache "no history" rather than treat it as a failure and fall through.
func TestChartsClient_Series_noCandlesIsAnEmptySeriesNotAnError(t *testing.T) {
	server := chartsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"candles":[]}`)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsNEW"}, "")
	series, err := client.Series(context.Background(), "NEWx", pyth.ChartRange1D, chartsNow)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 0 {
		t.Fatalf("points = %d, want 0", len(series.Points))
	}
	if series.EmptyReason == "" {
		t.Error("an empty series must say why it is empty")
	}
}

func TestChartsClient_Series_upstreamFailureIsAnError(t *testing.T) {
	server := chartsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate limited"}`)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
	if _, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, chartsNow); err == nil {
		t.Fatal("a 429 must be an error so the breaker trips and the fallback runs")
	}
}

func TestChartsClient_Series_unresolvableSymbolIsAnError(t *testing.T) {
	server := chartsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("Jupiter must not be called when the symbol has no mint")
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{err: errors.New("unknown symbol")}, "")
	if _, err := client.Series(context.Background(), "NOPEx", pyth.ChartRange1D, chartsNow); err == nil {
		t.Fatal("an unresolvable symbol must be an error")
	}
}

// A candle with a zero or negative close is dropped rather than drawn: a partial
// payload must not become a chart with a $0 spike in it.
func TestChartsClient_Series_dropsUnusableCandles(t *testing.T) {
	server := chartsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"candles":[
			{"time":%d,"open":230,"high":231,"low":229,"close":230},
			{"time":%d,"open":0,"high":0,"low":0,"close":0},
			{"time":%d,"open":231,"high":232,"low":230,"close":231}
		]}`,
			chartsNow.Add(-15*time.Minute).Unix(),
			chartsNow.Add(-10*time.Minute).Unix(),
			chartsNow.Add(-5*time.Minute).Unix(),
		)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
	series, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, chartsNow)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want 2 — the zero candle must be dropped", len(series.Points))
	}
	for _, point := range series.Points {
		if point.PriceUsdcMicros <= 0 {
			t.Errorf("a non-positive price survived: %d", point.PriceUsdcMicros)
		}
	}
}

func TestChartsClient_Series_toIsSentAsAnInstantJupiterAccepts(t *testing.T) {
	var got string
	server := chartsServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("to")
		fmt.Fprint(w, `{"candles":[]}`)
	})
	client := NewChartsClientWithBaseURL(server.URL, server.Client(), stubMints{mint: "XsAAPL"}, "")
	if _, err := client.Series(context.Background(), "AAPLx", pyth.ChartRange1D, chartsNow); err != nil {
		t.Fatalf("Series: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("to = %q is not an RFC3339 instant: %v", got, err)
	}
	if _, err := url.Parse("?to=" + url.QueryEscape(got)); err != nil {
		t.Errorf("to = %q does not survive query escaping: %v", got, err)
	}
}
