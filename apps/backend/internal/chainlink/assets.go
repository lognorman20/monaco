package chainlink

import (
	"context"
	"sort"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type assetPrices struct {
	live *liveClient
}

// NewAssetPrices returns catalog marks from Chainlink TRV feeds.
func NewAssetPrices(chain evm.Client, catalog b20.Catalog, now func() time.Time) pyth.AssetPriceClient {
	if now == nil {
		now = time.Now
	}
	return &assetPrices{live: &liveClient{chain: chain, catalog: catalog, now: now}}
}

func (a *assetPrices) AssetMark(ctx context.Context, symbol string) (pyth.AssetMark, error) {
	price, _, err := a.live.markSymbol(ctx, symbol)
	if err != nil {
		return pyth.AssetMark{}, err
	}
	return pyth.AssetMark{PriceUsdcMicros: price}, nil
}

func (a *assetPrices) AssetMarks(ctx context.Context, symbols []string) (map[string]pyth.AssetMark, error) {
	prices := a.live.markSymbols(ctx, symbols)
	out := make(map[string]pyth.AssetMark, len(prices))
	for symbol, price := range prices {
		if price <= 0 {
			continue
		}
		out[symbol] = pyth.AssetMark{PriceUsdcMicros: price}
	}
	return out, nil
}

// chartRoundDepth is how many rounds back the fallback reads. Total-return feeds
// update on deviation and on a daily heartbeat, so this is days of history, not
// months.
const chartRoundDepth = 48

// ChartSeries is the last fallback behind Pyth: the token's own total-return rounds.
// It is the token per token, so the series says so (Source chainlink, Basis token),
// and it only ever draws rounds inside the requested window. The rounds it can
// reach cover days, so 3M, 1Y and ALL come back empty: drawing two days of rounds
// under a "1Y" chip would be a year chart of something else.
func (a *assetPrices) ChartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, error) {
	empty := pyth.AssetChartSeries{EmptyReason: pyth.EmptyReasonNoHistory, Range: chartRange}
	window, ok := chartWindow(chartRange)
	if !ok {
		return empty, nil
	}
	if a.live == nil || a.live.catalog == nil || a.live.chain == nil {
		return empty, nil
	}
	feed, err := a.live.catalog.Feed(ctx, symbol)
	if err != nil {
		return empty, nil
	}
	rounds, err := a.live.chain.ChainlinkRoundHistory(ctx, feed, chartRoundDepth)
	if err != nil || len(rounds) == 0 {
		return empty, nil
	}
	now := a.live.now()
	cutoff := now.Add(-window)
	points := make([]pyth.ChartPoint, 0, len(rounds))
	for _, round := range rounds {
		if round.UpdatedAt.Before(cutoff) || round.UpdatedAt.After(now) {
			continue
		}
		price, _, convErr := roundToMark(now, round)
		if convErr != nil {
			continue
		}
		points = append(points, pyth.ChartPoint{
			Timestamp:       round.UpdatedAt.UTC().Unix(),
			PriceUsdcMicros: price,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	points = dedupeChartPoints(points)
	if len(points) < 2 {
		return empty, nil
	}
	return pyth.AssetChartSeries{
		Points:      points,
		Range:       chartRange,
		Source:      pyth.ChartSourceChainlink,
		Basis:       pyth.PriceBasisToken,
		BasisSymbol: symbol,
	}, nil
}

// chartWindow is how far back each range reaches. The second result is false for
// the ranges the rounds cannot honestly cover.
func chartWindow(chartRange pyth.ChartRange) (time.Duration, bool) {
	switch chartRange {
	case pyth.ChartRange1D:
		return 24 * time.Hour, true
	case pyth.ChartRange1W:
		return 7 * 24 * time.Hour, true
	case pyth.ChartRange1M:
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func dedupeChartPoints(points []pyth.ChartPoint) []pyth.ChartPoint {
	if len(points) == 0 {
		return points
	}
	out := points[:1]
	for _, p := range points[1:] {
		if p.Timestamp == out[len(out)-1].Timestamp {
			out[len(out)-1] = p
			continue
		}
		out = append(out, p)
	}
	return out
}
