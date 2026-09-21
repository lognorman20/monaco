package pyth

import (
	"context"
	"fmt"
	"sync"
)

type fakeAssetPriceClient struct {
	mu sync.Mutex

	marks   map[string]AssetMark
	markErr map[string]error
	charts  map[string]AssetChartSeries
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

func (f *fakeAssetPriceClient) AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error) {
	out := make(map[string]AssetMark, len(symbols))
	for _, symbol := range symbols {
		mark, err := f.AssetMark(ctx, symbol)
		if err != nil || mark.PriceUsdcMicros <= 0 {
			continue
		}
		out[symbol] = mark
	}
	return out, nil
}

func (f *fakeAssetPriceClient) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	_ = ctx
	key := chartKey(symbol, chartRange)
	f.mu.Lock()
	series, ok := f.charts[key]
	f.mu.Unlock()
	if !ok {
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory}, nil
	}
	return series, nil
}

// DayChange derives the day change from the registered 1D series, the way the
// real client does from Benchmarks.
func (f *fakeAssetPriceClient) DayChange(ctx context.Context, symbol string) *string {
	series, _ := f.ChartSeries(ctx, symbol, ChartRange1D)
	return DayChange(series)
}

// DaySeries serves the registered 1D series. A symbol with nothing registered is
// Benchmarks failing to answer, so it reports false, the way the real client does
// on an outage.
func (f *fakeAssetPriceClient) DaySeries(ctx context.Context, symbol string) (AssetChartSeries, bool) {
	_ = ctx
	f.mu.Lock()
	series, ok := f.charts[chartKey(symbol, ChartRange1D)]
	f.mu.Unlock()
	return series, ok
}

type fakeEquityQuoteClient struct {
	mu     sync.Mutex
	quotes map[string]ReferenceQuote
}

// NewFakeEquityQuoteClient returns an in-memory equity reference client for tests.
// An unconfigured symbol answers as a symbol with no Pyth feed.
func NewFakeEquityQuoteClient() EquityQuoteClient {
	return &fakeEquityQuoteClient{quotes: make(map[string]ReferenceQuote)}
}

// RegisterEquityQuote configures the equity reference line for a symbol.
func RegisterEquityQuote(client EquityQuoteClient, symbol string, quote ReferenceQuote) {
	fake, ok := client.(*fakeEquityQuoteClient)
	if !ok {
		panic("pyth: RegisterEquityQuote requires NewFakeEquityQuoteClient")
	}
	fake.mu.Lock()
	fake.quotes[normalizeSymbol(symbol)] = quote
	fake.mu.Unlock()
}

func (f *fakeEquityQuoteClient) EquityQuote(ctx context.Context, symbol string) ReferenceQuote {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	quote, ok := f.quotes[normalizeSymbol(symbol)]
	if !ok {
		return unavailableQuote(QuoteSourcePythEquity, QuoteReasonNoFeed)
	}
	return quote
}
