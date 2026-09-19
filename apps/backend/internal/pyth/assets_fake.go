package pyth

import (
	"context"
	"fmt"
	"sync"
)

type fakeAssetPriceClient struct {
	mu sync.Mutex

	marks  map[string]AssetMark
	markErr map[string]error
	charts map[string]AssetChartSeries
}

// NewFakeAssetPriceClient returns an in-memory asset price client for tests.
func NewFakeAssetPriceClient() AssetPriceClient {
	return &fakeAssetPriceClient{
		marks:   make(map[string]AssetMark),
		markErr: make(map[string]error),
		charts:  make(map[string]AssetChartSeries),
	}
}

// RegisterAssetMark configures AssetMark for a symbol.
func RegisterAssetMark(client AssetPriceClient, symbol string, mark AssetMark) {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: RegisterAssetMark requires NewFakeAssetPriceClient")
	}
	key := normalizeSymbol(symbol)
	fake.mu.Lock()
	fake.marks[key] = mark
	delete(fake.markErr, key)
	fake.mu.Unlock()
}

// RegisterAssetMarkError forces AssetMark to return err for a symbol.
func RegisterAssetMarkError(client AssetPriceClient, symbol string, err error) {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: RegisterAssetMarkError requires NewFakeAssetPriceClient")
	}
	key := normalizeSymbol(symbol)
	fake.mu.Lock()
	fake.markErr[key] = err
	delete(fake.marks, key)
	fake.mu.Unlock()
}

// RegisterChartSeries configures ChartSeries for a symbol and range key.
func RegisterChartSeries(client AssetPriceClient, symbol string, chartRange ChartRange, series AssetChartSeries) {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: RegisterChartSeries requires NewFakeAssetPriceClient")
	}
	key := chartKey(symbol, chartRange)
	fake.mu.Lock()
	fake.charts[key] = series
	fake.mu.Unlock()
}

func chartKey(symbol string, chartRange ChartRange) string {
	return normalizeSymbol(symbol) + ":" + string(chartRange)
}

func (f *fakeAssetPriceClient) AssetMark(ctx context.Context, symbol string) (AssetMark, error) {
	_ = ctx
	key := normalizeSymbol(symbol)
	f.mu.Lock()
	if err, ok := f.markErr[key]; ok {
		f.mu.Unlock()
		return AssetMark{}, err
	}
	mark, ok := f.marks[key]
	f.mu.Unlock()
	if !ok {
		return AssetMark{}, fmt.Errorf("pyth: asset mark not configured for %s", symbol)
	}
	return mark, nil
}

func (f *fakeAssetPriceClient) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	_ = ctx
	key := chartKey(symbol, chartRange)
	f.mu.Lock()
	series, ok := f.charts[key]
	f.mu.Unlock()
	if !ok {
		return AssetChartSeries{EmptyReason: "price history unavailable"}, nil
	}
	return series, nil
}
