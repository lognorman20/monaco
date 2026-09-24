package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// DefaultSparkWarmInterval keeps the pinned symbols' day series inside the chart
// cache's TTL without asking Hermes more often than the cache would answer from
// memory anyway.
const DefaultSparkWarmInterval = 5 * time.Minute

// sparkWarmTickBudget bounds one pass. A warmer that outran its own interval
// would stack passes on top of each other, which is how a background job becomes
// an outage.
const sparkWarmTickBudget = 90 * time.Second

// sparkWarmConcurrency is deliberately low. Nobody is waiting on this pass, and
// the point is to leave rate-limit headroom for the requests someone *is* waiting
// on.
const sparkWarmConcurrency = 3

// SparkWarmer keeps the popular symbols' 1D series hot in the chart cache, so a
// list route serves every row's sparkline from memory.
//
// Without it the first person to open the Stocks tab after a cache expiry pays
// for the whole page's history, and the rows they are looking at are precisely
// the ones that arrive without a sparkline. The list is built to survive that —
// a row with no series draws no line — but "survives it" is not "should meet it".
type SparkWarmer struct {
	catalog xstocks.CatalogSearcher
	prices  pyth.AssetPriceClient
	limit   int
}

// NewSparkWarmer returns nil when there is nothing to warm with, so the caller
// can wire it unconditionally and start it only when it exists.
func NewSparkWarmer(catalog xstocks.CatalogSearcher, prices pyth.AssetPriceClient, limit int) *SparkWarmer {
	if catalog == nil || prices == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	return &SparkWarmer{catalog: catalog, prices: prices, limit: limit}
}

// Tick warms one pass. It never returns an error worth failing on: this is a
// cache fill, and a symbol that did not warm simply is not warm.
func (w *SparkWarmer) Tick(ctx context.Context) error {
	if w == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, sparkWarmTickBudget)
	defer cancel()

	assets, err := xstocks.Popular(ctx, w.catalog, w.limit)
	if err != nil {
		return err
	}

	slots := make(chan struct{}, sparkWarmConcurrency)
	done := make(chan struct{}, len(assets))
	started := 0
	for _, asset := range assets {
		symbol := asset.Symbol
		if symbol == "" {
			continue
		}
		started++
		go func(symbol string) {
			defer func() { done <- struct{}{} }()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			if _, err := w.prices.ChartSeries(ctx, symbol, pyth.ChartRange1D); err != nil {
				slog.Debug("spark warm failed", "symbol", symbol, "err", err)
			}
		}(symbol)
	}
	for i := 0; i < started; i++ {
		<-done
	}
	slog.Info("spark warm pass complete", "symbol_count", started)
	return nil
}

// RunSparkWarmer warms once immediately, then on every interval, until ctx ends.
func RunSparkWarmer(ctx context.Context, warmer *SparkWarmer, interval time.Duration) {
	if warmer == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSparkWarmInterval
	}
	telemetry.RegisterPoller(PollerSparkWarm, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runTick := func() {
		telemetry.GuardTick(ctx, PollerSparkWarm, func() error {
			err := warmer.Tick(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Warn("spark warm tick failed", "err", err)
			}
			return err
		})
	}

	// The first pass runs at boot rather than after the first interval: the cache
	// is coldest the moment the process starts, which is when the first person to
	// open the tab would otherwise pay for it.
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
