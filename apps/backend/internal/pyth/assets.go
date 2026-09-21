package pyth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// ChartRange is a supported asset chart window.
type ChartRange string

const (
	ChartRange1D ChartRange = "1D"
	ChartRange1W ChartRange = "1W"
	ChartRange1M ChartRange = "1M"
)

// AssetMark is a standalone equity mark for catalog pricing.
type AssetMark struct {
	PriceUsdcMicros int64
	Change24h       *string
}

// ChartPoint is one chart sample.
type ChartPoint struct {
	Timestamp       int64 `json:"timestamp"`
	PriceUsdcMicros int64 `json:"priceUsdcMicros"`
}

// AssetChartSeries is a time series for Swift Charts.
type AssetChartSeries struct {
	Points      []ChartPoint `json:"points"`
	EmptyReason string       `json:"emptyReason,omitempty"`
}

// AssetPriceClient fetches standalone marks and chart history.
type AssetPriceClient interface {
	AssetMark(ctx context.Context, symbol string) (AssetMark, error)
	AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error)
	ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error)
}

func (c *HermesClient) AssetMark(ctx context.Context, symbol string) (AssetMark, error) {
	marked, err := c.markHolding(ctx, CostBasis{
		Symbol: symbol,
		Units:  1,
		Price:  0,
		Amount: 1,
	})
	if err != nil {
		return AssetMark{}, err
	}

	mark := AssetMark{PriceUsdcMicros: marked.MarkUsdc}
	if change, ok, err := c.change24h(ctx, symbol, marked.MarkUsdc); err == nil && ok {
		mark.Change24h = &change
	}
	return mark, nil
}

func (c *HermesClient) AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error) {
	out := make(map[string]AssetMark, len(symbols))
	for _, symbol := range symbols {
		mark, err := c.AssetMark(ctx, symbol)
		if err != nil || mark.PriceUsdcMicros <= 0 {
			continue
		}
		out[symbol] = mark
	}
	return out, nil
}

func (c *HermesClient) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	if equityFeedsDenied() {
		return AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	feedID, _, err := c.resolveFeedSession(ctx, symbol)
	if err != nil {
		markEquityDenied(err)
		return AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}

	samples := chartSampleTimes(chartRange, time.Now().UTC())
	points := make([]ChartPoint, 0, len(samples))
	for _, ts := range samples {
		price, err := c.fetchHistoricalPrice(ctx, feedID, ts)
		if err != nil {
			if markEquityDenied(err) {
				break
			}
			continue
		}
		points = append(points, ChartPoint{
			Timestamp:       ts.Unix(),
			PriceUsdcMicros: price,
		})
	}
	if len(points) < 2 && !equityFeedsDenied() {
		if latest, err := c.fetchLatestPrice(ctx, feedID); err == nil {
			if price, err := priceToUSDCMicros(latest.Price.Price, latest.Price.Expo); err == nil && price > 0 {
				now := time.Now().UTC().Unix()
				points = []ChartPoint{
					{Timestamp: now - 3600, PriceUsdcMicros: price},
					{Timestamp: now, PriceUsdcMicros: price},
				}
			}
		}
	}
	if len(points) < 2 {
		return AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	return AssetChartSeries{Points: points}, nil
}

func chartSampleTimes(chartRange ChartRange, now time.Time) []time.Time {
	switch chartRange {
	case ChartRange1W:
		return dailySamples(now, 7)
	case ChartRange1M:
		return dailySamples(now, 30)
	default:
		return hourlySamples(now, 12)
	}
}

func hourlySamples(now time.Time, hours int) []time.Time {
	out := make([]time.Time, 0, hours+1)
	step := time.Hour
	if hours <= 12 {
		step = 2 * time.Hour
	}
	span := time.Duration(hours) * step
	start := now.Add(-span).Truncate(step)
	for t := start; !t.After(now); t = t.Add(step) {
		out = append(out, t)
	}
	return out
}

func dailySamples(now time.Time, days int) []time.Time {
	out := make([]time.Time, 0, days)
	start := now.AddDate(0, 0, -days).Truncate(24 * time.Hour)
	for i := 0; i <= days; i++ {
		out = append(out, start.AddDate(0, 0, i))
	}
	return out
}

func (c *HermesClient) change24h(ctx context.Context, symbol string, currentMicros int64) (string, bool, error) {
	feedID, _, err := c.resolveFeedSession(ctx, symbol)
	if err != nil {
		return "", false, err
	}
	prior, err := c.fetchHistoricalPrice(ctx, feedID, time.Now().UTC().Add(-24*time.Hour))
	if err != nil || prior <= 0 {
		return "", false, err
	}
	delta := float64(currentMicros-prior) / float64(prior)
	return formatDecimalRatio(delta), true, nil
}

func formatDecimalRatio(ratio float64) string {
	return fmt.Sprintf("%.6f", ratio)
}

func (c *HermesClient) fetchHistoricalPrice(ctx context.Context, feedID string, at time.Time) (int64, error) {
	endpoint := c.baseURL + hermesPricePath("historical", feedID, at.Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	if err := c.setHermesAuth(req); err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		histErr := hermesRequestError("pyth historical price", resp.StatusCode, body)
		markEquityDenied(histErr)
		return 0, histErr
	}

	var payload latestPriceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if len(payload.Parsed) == 0 {
		return 0, fmt.Errorf("pyth historical price: empty parsed payload")
	}
	return priceToUSDCMicros(payload.Parsed[0].Price.Price, payload.Parsed[0].Price.Expo)
}

// ParseChartRange validates a chart range query param.
func ParseChartRange(raw string) (ChartRange, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(ChartRange1D), "":
		return ChartRange1D, nil
	case string(ChartRange1W):
		return ChartRange1W, nil
	case string(ChartRange1M):
		return ChartRange1M, nil
	default:
		return "", fmt.Errorf("invalid chart range %q", raw)
	}
}

// MidSpreadBps compares a Jupiter implied price to a Pyth mid mark.
func MidSpreadBps(markMicros int64, jupiterOutAmount string, jupiterInMicros int64, outputDecimals int32) *int {
	if markMicros <= 0 || jupiterInMicros <= 0 {
		return nil
	}
	outRaw, err := parseDecimalInt(strings.TrimSpace(jupiterOutAmount))
	if err != nil || outRaw <= 0 {
		return nil
	}
	scale := int64(math.Pow10(int(outputDecimals)))
	if scale <= 0 {
		return nil
	}
	jupiterMicrosPerShare := (jupiterInMicros * scale) / outRaw
	if jupiterMicrosPerShare <= 0 {
		return nil
	}
	spread := float64(jupiterMicrosPerShare-markMicros) / float64(markMicros)
	bps := int(math.Round(spread * 10_000))
	return &bps
}
