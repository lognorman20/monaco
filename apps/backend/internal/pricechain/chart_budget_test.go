package pricechain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// slowCharts is a history source that takes as long as it is told to. It stands
// for a cold Benchmarks minute, which is where the 20s fetch timeout used to be
// spent — with the caller's own budget discarded on the way in.
type slowCharts struct {
	keylessCharts
	delay   time.Duration
	entered atomic.Int32
}

func newSlowCharts(delay time.Duration) *slowCharts {
	charts := &slowCharts{delay: delay}
	charts.sourceUp.Store(true)
	return charts
}

func (s *slowCharts) ChartSeries(ctx context.Context, _ string, _ pyth.ChartRange) (pyth.AssetChartSeries, error) {
	s.entered.Add(1)
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return pyth.AssetChartSeries{}, ctx.Err()
	}
	return pyth.AssetChartSeries{Points: []pyth.ChartPoint{
		{Timestamp: 1, PriceUsdcMicros: 226_500_000},
		{Timestamp: 2, PriceUsdcMicros: 231_400_000},
	}}, nil
}

// waitForCachedSeries polls the cache-only read until the background fill lands.
func waitForCachedSeries(t *testing.T, chain *Chain, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, ok := chain.CachedChartSeries(context.Background(), testSymbol, pyth.ChartRange1D); ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// The regression: a caller's deadline was observed nowhere once ChartSeries was
// entered. The fetch re-rooted on context.WithoutCancel and the wait was a plain
// `<-call.done`, so a documented 1.2s budget meant waiting out FetchTimeout —
// 20 seconds in production, per wave of six, on a screen that polls.
func TestChain_ChartSeries_honoursTheCallersDeadline(t *testing.T) {
	t.Parallel()
	chain := New(nil, nil, newSlowCharts(5*time.Second), testConfig(newFakeClock()))

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := chain.ChartSeries(ctx, testSymbol, pyth.ChartRange1D)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("the caller waited %s, well past its own 80ms budget", elapsed)
	}
}

// Abandoning the wait must not abandon the fetch: it is the thing that fills the
// cache for the next caller, and throwing it away would mean a busy screen with
// short budgets never warms anything at all.
func TestChain_ChartSeries_theSharedFetchOutlivesAnAbandonedCaller(t *testing.T) {
	t.Parallel()
	charts := newSlowCharts(150 * time.Millisecond)
	chain := New(nil, nil, charts, testConfig(newFakeClock()))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := chain.ChartSeries(ctx, testSymbol, pyth.ChartRange1D); err == nil {
		t.Fatal("want the caller's deadline error")
	}

	if !waitForCachedSeries(t, chain, 3*time.Second) {
		t.Fatal("the abandoned fetch never finished, so nothing was cached")
	}
	if got := charts.entered.Load(); got != 1 {
		t.Fatalf("upstream fetches = %d, want exactly 1", got)
	}
}

// What a list row reads: memory or nothing, never a vendor.
func TestChain_CachedChartSeries_missesWithoutBlockingAndWarmsForNextTime(t *testing.T) {
	t.Parallel()
	chain := New(nil, nil, newSlowCharts(120*time.Millisecond), testConfig(newFakeClock()))

	start := time.Now()
	if _, ok := chain.CachedChartSeries(context.Background(), testSymbol, pyth.ChartRange1D); ok {
		t.Fatal("nothing is cached yet; want a miss")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("a cache-only read took %s; it must not wait on the fill", elapsed)
	}

	if !waitForCachedSeries(t, chain, 3*time.Second) {
		t.Fatal("the background warm never landed")
	}
}

// A page of twenty cold rows must not become twenty simultaneous vendor calls for
// the same symbol.
func TestChain_CachedChartSeries_aPageOfMissesStartsOneFillPerSymbol(t *testing.T) {
	t.Parallel()
	charts := newSlowCharts(100 * time.Millisecond)
	chain := New(nil, nil, charts, testConfig(newFakeClock()))

	for i := 0; i < 20; i++ {
		chain.CachedChartSeries(context.Background(), testSymbol, pyth.ChartRange1D)
	}

	if !waitForCachedSeries(t, chain, 3*time.Second) {
		t.Fatal("the background warm never landed")
	}
	if got := charts.entered.Load(); got != 1 {
		t.Fatalf("upstream fetches = %d, want 1", got)
	}
}

// A chart client with no cache at all must still not be reached through the
// cache-only door.
func TestChain_CachedChartSeries_withNoChartClientIsAMiss(t *testing.T) {
	t.Parallel()
	chain := New(nil, nil, nil, testConfig(newFakeClock()))
	if _, ok := chain.CachedChartSeries(context.Background(), testSymbol, pyth.ChartRange1D); ok {
		t.Fatal("want a miss when there is no chart client")
	}
}
