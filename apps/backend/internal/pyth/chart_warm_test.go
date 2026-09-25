package pyth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingSeriesSource answers every range with one point and counts the calls per
// range. With a gate set, every call blocks until the gate is closed, which is how
// a test holds a warm open while it asks for a second one.
type countingSeriesSource struct {
	mu    sync.Mutex
	calls map[ChartRange]int
	gate  chan struct{}
	err   error
}

func newCountingSeriesSource() *countingSeriesSource {
	return &countingSeriesSource{calls: make(map[ChartRange]int)}
}

func (s *countingSeriesSource) Series(ctx context.Context, _ string, chartRange ChartRange, now time.Time) (AssetChartSeries, error) {
	s.mu.Lock()
	s.calls[chartRange]++
	gate, err := s.gate, s.err
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return AssetChartSeries{}, ctx.Err()
		}
	}
	if err != nil {
		return AssetChartSeries{}, err
	}
	return AssetChartSeries{
		Points: []ChartPoint{{Timestamp: now.Unix(), PriceUsdcMicros: 231_400_000}},
		Range:  chartRange,
		Source: ChartSourceYahoo,
	}, nil
}

func (s *countingSeriesSource) callsFor(chartRange ChartRange) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[chartRange]
}

func (s *countingSeriesSource) totalCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, n := range s.calls {
		total += n
	}
	return total
}

// warmTestClient is a chart client whose Hermes host is unreachable, so anything
// that fell through to the per-sample path would fail loudly instead of quietly
// costing thirty requests. The pause is zeroed so the tests do not sleep.
func warmTestClient(source SeriesSource) *HermesClient {
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "")
	if source != nil {
		client.WithSeriesSource(source)
	}
	client.rangeWarmPause = 0
	return client
}

func waitForWarm(t *testing.T, done <-chan struct{}) {
	t.Helper()
	if done == nil {
		t.Fatal("expected a warm to start")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("warm did not finish")
	}
}

func TestWarmChartRanges_afterTheFirstRangeEveryRangeIsCachedAndFetchedOnce(t *testing.T) {
	// The first tap on another chip used to go upstream on demand, and one of those
	// taps waited fifteen seconds on a rate-limited vendor. After the screen's first
	// range is served, one warm must leave every chip answering from memory.
	source := newCountingSeriesSource()
	client := warmTestClient(source)

	if _, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1D); err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	waitForWarm(t, client.startChartRangeWarm("AAPLx"))

	for _, chartRange := range ChartRanges {
		if _, ok := client.chartCache.Get("AAPLx", chartRange); !ok {
			t.Errorf("%s is not cached after the warm", chartRange)
		}
		if got := source.callsFor(chartRange); got != 1 {
			t.Errorf("%s fetched %d times, want 1", chartRange, got)
		}
	}
}

func TestWarmChartRanges_aSecondRequestWhileWarmingStartsNoSecondWarmer(t *testing.T) {
	// The asset screen polls and every chip tap is a chart request, so the warm is
	// asked for over and over while it runs. Only the first may start one.
	source := newCountingSeriesSource()
	source.gate = make(chan struct{})
	client := warmTestClient(source)

	first := client.startChartRangeWarm("AAPLx")
	if first == nil {
		t.Fatal("expected the first request to start a warm")
	}
	if second := client.startChartRangeWarm("aaplx"); second != nil {
		t.Fatal("a second warm started for a symbol that is already warming")
	}
	close(source.gate)
	waitForWarm(t, first)

	for _, chartRange := range ChartRanges {
		if got := source.callsFor(chartRange); got != 1 {
			t.Errorf("%s fetched %d times, want 1: a duplicate warmer ran", chartRange, got)
		}
	}
	// Once the warm has finished and everything is cached, asking again is free.
	if again := client.startChartRangeWarm("AAPLx"); again != nil {
		t.Fatal("a warm started for a symbol whose ranges are all cached")
	}
}

func TestWarmChartRanges_capsHowManySymbolsWarmAtOnce(t *testing.T) {
	// The cap is what keeps several screens opening together from becoming the
	// burst the warm exists to avoid.
	source := newCountingSeriesSource()
	source.gate = make(chan struct{})
	client := warmTestClient(source)

	var running []<-chan struct{}
	for _, symbol := range []string{"AAPLx", "TSLAx"} {
		done := client.startChartRangeWarm(symbol)
		if done == nil {
			t.Fatalf("expected %s to start a warm", symbol)
		}
		running = append(running, done)
	}
	if extra := client.startChartRangeWarm("NVDAx"); extra != nil {
		t.Fatal("a warm started past the concurrency cap")
	}
	close(source.gate)
	for _, done := range running {
		waitForWarm(t, done)
	}
	if extra := client.startChartRangeWarm("NVDAx"); extra == nil {
		t.Fatal("a dropped warm must be startable again once a slot frees up")
	} else {
		waitForWarm(t, extra)
	}
}

func TestWarmChartRanges_anAlreadyCachedRangeIsNotRefetched(t *testing.T) {
	source := newCountingSeriesSource()
	client := warmTestClient(source)
	ctx := context.Background()

	// The detail route has already asked for the day and the year.
	for _, chartRange := range []ChartRange{ChartRange1D, ChartRange1Y} {
		if _, err := client.ChartSeries(ctx, "AAPLx", chartRange); err != nil {
			t.Fatalf("ChartSeries %s: %v", chartRange, err)
		}
	}
	waitForWarm(t, client.startChartRangeWarm("AAPLx"))

	for _, chartRange := range ChartRanges {
		if got := source.callsFor(chartRange); got != 1 {
			t.Errorf("%s fetched %d times, want 1", chartRange, got)
		}
	}
}

func TestWarmChartRanges_skippedWithoutASeriesSource(t *testing.T) {
	// Without a one-call source the only history path is the Hermes sampler, at
	// about thirty requests a range. A warm must never pay that.
	var hermesCalls atomic.Int32
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hermesCalls.Add(1)
		http.NotFound(w, r)
	}))
	defer hermes.Close()

	client := NewHermesClientWithHTTP(hermes.URL, hermes.Client(), "test-pyth-key")
	client.rangeWarmPause = 0
	if done := client.startChartRangeWarm("AAPLx"); done != nil {
		t.Fatal("a warm started with no series source")
	}
	client.WarmChartRanges("AAPLx")
	if got := hermesCalls.Load(); got != 0 {
		t.Fatalf("hermes received %d requests from a warm, want none", got)
	}
	for _, chartRange := range ChartRanges {
		if _, ok := client.chartCache.Get("AAPLx", chartRange); ok {
			t.Errorf("%s was cached by a warm that should not have run", chartRange)
		}
	}
}

func TestWarmChartRanges_offWhenDisabled(t *testing.T) {
	source := newCountingSeriesSource()
	client := warmTestClient(source).WithoutChartRangeWarm()

	if done := client.startChartRangeWarm("AAPLx"); done != nil {
		t.Fatal("a warm started on a client with warming turned off")
	}
	if got := source.totalCalls(); got != 0 {
		t.Fatalf("source called %d times, want none", got)
	}
}

func TestWarmChartRanges_stopsAtTheFirstFailureAndTripsTheBreaker(t *testing.T) {
	// A source that is failing is not asked for the other four ranges, and the
	// breaker it trips is the same one the chart path reads, so neither path asks
	// again until the cooldown is over.
	source := newCountingSeriesSource()
	source.err = errors.New("yahoo charts AAPL: status 429")
	client := warmTestClient(source)

	waitForWarm(t, client.startChartRangeWarm("AAPLx"))

	if got := source.totalCalls(); got != 1 {
		t.Fatalf("source called %d times, want 1: the warm must stop at the first failure", got)
	}
	if client.seriesBreaker.allows(time.Now()) {
		t.Fatal("the failed warm did not trip the series breaker")
	}
	if done := client.startChartRangeWarm("AAPLx"); done != nil {
		t.Fatal("a warm started while the series breaker is open")
	}
	if got := source.totalCalls(); got != 1 {
		t.Fatalf("source called %d times with the breaker open, want 1", got)
	}
}
