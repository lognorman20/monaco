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

func (a *assetPrices) ChartSeries(ctx context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, error) {
	if a.live == nil || a.live.catalog == nil || a.live.chain == nil {
		return pyth.AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	feed, err := a.live.catalog.Feed(ctx, symbol)
	if err != nil {
		return pyth.AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	rounds, err := a.live.chain.ChainlinkRoundHistory(ctx, feed, 48)
	if err != nil || len(rounds) == 0 {
		return pyth.AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	now := a.live.now()
	cutoff := now.Add(-chartWindow(chartRange))
	points := make([]pyth.ChartPoint, 0, len(rounds))
	for _, round := range rounds {
		price, _, convErr := roundToMark(now, round)
		if convErr != nil {
			continue
		}
		if round.UpdatedAt.Before(cutoff) {
			continue
		}
		points = append(points, pyth.ChartPoint{
			Timestamp:       round.UpdatedAt.Unix(),
			PriceUsdcMicros: price,
		})
	}
	if len(points) < 2 {
		points = points[:0]
		for _, round := range rounds {
			price, _, convErr := roundToMark(now, round)
			if convErr != nil {
				continue
			}
			points = append(points, pyth.ChartPoint{
				Timestamp:       round.UpdatedAt.Unix(),
				PriceUsdcMicros: price,
			})
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Timestamp < points[j].Timestamp })
	points = dedupeChartPoints(points)
	if len(points) < 2 {
		return pyth.AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	return pyth.AssetChartSeries{Points: points}, nil
}

func chartWindow(chartRange pyth.ChartRange) time.Duration {
	switch chartRange {
	case pyth.ChartRange1W:
		return 7 * 24 * time.Hour
	case pyth.ChartRange1M:
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
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
