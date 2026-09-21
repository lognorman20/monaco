package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// DefaultSparkWarmInterval refreshes the popular symbols' day series inside the
// 1D chart cache's lifetime (pyth.ChartCacheDayTTL, a minute), so an entry is
// replaced before it can lapse and a list page never meets a cold one. With the
// eight pinned popular symbols that is about ten Benchmarks calls a minute.
const DefaultSparkWarmInterval = 45 * time.Second

// sparkWarmTickBudget bounds one pass below the interval. A warmer that outran
// its own interval would stack passes on top of each other.
const sparkWarmTickBudget = 30 * time.Second

// sparkWarmConcurrency is deliberately low. Nobody is waiting on this pass, and
// the point is to leave headroom for the requests someone is waiting on.
const sparkWarmConcurrency = 3

// SparkWarmer keeps the popular symbols' 1D Pyth series fresh in the chart cache,
// so the Stocks list serves every popular row's day move and sparkline from
// memory.
//
// Without it the first person to open the Stocks tab after an entry expires pays
// for the page's history under the list's budget, and the rows they are looking
// at are precisely the ones that may arrive without a line. The list survives
// that (a row with no series draws no line), but surviving it is not the same as
// never meeting it.
type SparkWarmer struct {
	catalog b20.Catalog
	series  pyth.DaySeriesWarmer
}

// NewSparkWarmer returns nil when there is nothing to warm with, so the caller
// can wire it unconditionally and start it only when it exists.
func NewSparkWarmer(catalog b20.Catalog, series pyth.DaySeriesWarmer) *SparkWarmer {
	if catalog == nil || series == nil {
		return nil
	}
	return &SparkWarmer{catalog: catalog, series: series}
}

// Tick refreshes one pass over catalog.Popular and reports how many symbols
// Benchmarks answered for. A symbol that did not warm is simply not warm; only a
// catalog that cannot list the popular symbols is an error.
func (w *SparkWarmer) Tick(ctx context.Context) (warmed, attempted int, err error) {
	if w == nil {
		return 0, 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, sparkWarmTickBudget)
	defer cancel()

	assets, err := w.catalog.Popular(ctx)
	if err != nil {
		return 0, 0, err
	}

	slots := make(chan struct{}, sparkWarmConcurrency)
	results := make(chan bool, len(assets))
	started := 0
	for _, asset := range assets {
		symbol := asset.Symbol
		if symbol == "" {
			continue
		}
		started++
		go func(symbol string) {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results <- false
				return
			}
			results <- w.series.WarmDaySeries(ctx, symbol)
		}(symbol)
	}
	warmed = 0
	for i := 0; i < started; i++ {
		if <-results {
			warmed++
		}
	}
	return warmed, started, nil
}

// RunSparkWarmer warms once immediately, then on every interval, until ctx ends.
func RunSparkWarmer(ctx context.Context, warmer *SparkWarmer, interval time.Duration) {
	if warmer == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSparkWarmInterval
	}
	slog.Info("spark warmer started", "interval", interval.String())
	defer slog.Info("spark warmer stopped")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runTick := func() {
		warmed, attempted, err := warmer.Tick(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Warn("spark warm tick failed", "err", err.Error())
		case err != nil:
		case attempted > 0 && warmed == 0 && ctx.Err() == nil:
			// Every popular symbol failed: Benchmarks is down or its breaker is
			// open, and the list is serving rows without day moves or lines.
			slog.Warn("spark warm tick warmed nothing", "attempted", attempted)
		default:
			slog.Debug("spark warm tick", "warmed", warmed, "attempted", attempted)
		}
	}

	// The first pass runs at boot rather than after the first interval: the cache
	// is coldest the moment the process starts.
	runTick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		}
	}
}
