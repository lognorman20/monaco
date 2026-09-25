package pyth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type fakeAssetPriceClient struct {
	mu sync.Mutex

	marks      map[string]AssetMark
	markErr    map[string]error
	charts     map[string]AssetChartSeries
	chartDelay map[string]time.Duration
	chartCalls map[string]int
	warmCalls  map[string]int
}

// NewFakeAssetPriceClient returns an in-memory asset price client for tests.
func NewFakeAssetPriceClient() AssetPriceClient {
	return &fakeAssetPriceClient{
		marks:      make(map[string]AssetMark),
		markErr:    make(map[string]error),
		charts:     make(map[string]AssetChartSeries),
		chartDelay: make(map[string]time.Duration),
		chartCalls: make(map[string]int),
		warmCalls:  make(map[string]int),
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

// RegisterChartSeriesDelay makes ChartSeries take delay to answer for a symbol,
// so a test can stand a slow vendor up and check that a caller's budget is really
// its budget.
func RegisterChartSeriesDelay(client AssetPriceClient, symbol string, chartRange ChartRange, delay time.Duration) {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: RegisterChartSeriesDelay requires NewFakeAssetPriceClient")
	}
	key := chartKey(symbol, chartRange)
	fake.mu.Lock()
	fake.chartDelay[key] = delay
	fake.mu.Unlock()
}

// ChartSeriesCallCount reports how many times a symbol's series was fetched.
func ChartSeriesCallCount(client AssetPriceClient, symbol string, chartRange ChartRange) int {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: ChartSeriesCallCount requires NewFakeAssetPriceClient")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.chartCalls[chartKey(symbol, chartRange)]
}

// ChartRangeWarmCount reports how many times a chart range warm was asked for a
// symbol.
func ChartRangeWarmCount(client AssetPriceClient, symbol string) int {
	fake, ok := client.(*fakeAssetPriceClient)
	if !ok {
		panic("pyth: ChartRangeWarmCount requires NewFakeAssetPriceClient")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.warmCalls[normalizeSymbol(symbol)]
}

// WarmChartRanges records the request and fetches nothing.
func (f *fakeAssetPriceClient) WarmChartRanges(symbol string) {
	f.mu.Lock()
	f.warmCalls[normalizeSymbol(symbol)]++
	f.mu.Unlock()
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
	key := chartKey(symbol, chartRange)
	f.mu.Lock()
	series, ok := f.charts[key]
	delay := f.chartDelay[key]
	f.chartCalls[key]++
	f.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return AssetChartSeries{}, ctx.Err()
		}
	}
	if !ok {
		return AssetChartSeries{EmptyReason: EmptyReasonNoHistory}, nil
	}
	return series, nil
}

type fakeReferenceQuoteClient struct {
	mu     sync.Mutex
	quotes map[string]ReferenceQuotes
	errs   map[string]error
}

// NewFakeReferenceQuoteClient returns an in-memory stock-vs-token client for tests.
func NewFakeReferenceQuoteClient() ReferenceQuoteClient {
	return &fakeReferenceQuoteClient{
		quotes: make(map[string]ReferenceQuotes),
		errs:   make(map[string]error),
	}
}

// RegisterReferenceQuotes configures the pair returned for a symbol.
func RegisterReferenceQuotes(client ReferenceQuoteClient, symbol string, quotes ReferenceQuotes) {
	fake, ok := client.(*fakeReferenceQuoteClient)
	if !ok {
		panic("pyth: RegisterReferenceQuotes requires NewFakeReferenceQuoteClient")
	}
	key := normalizeSymbol(symbol)
	fake.mu.Lock()
	fake.quotes[key] = quotes
	delete(fake.errs, key)
	fake.mu.Unlock()
}

// RegisterReferenceQuotesError forces the fetch to fail for a symbol.
func RegisterReferenceQuotesError(client ReferenceQuoteClient, symbol string, err error) {
	fake, ok := client.(*fakeReferenceQuoteClient)
	if !ok {
		panic("pyth: RegisterReferenceQuotesError requires NewFakeReferenceQuoteClient")
	}
	key := normalizeSymbol(symbol)
	fake.mu.Lock()
	fake.errs[key] = err
	delete(fake.quotes, key)
	fake.mu.Unlock()
}

func (f *fakeReferenceQuoteClient) ReferenceQuotes(ctx context.Context, symbol string) (ReferenceQuotes, error) {
	_ = ctx
	key := normalizeSymbol(symbol)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.errs[key]; ok {
		return ReferenceQuotes{}, err
	}
	quotes, ok := f.quotes[key]
	if !ok {
		// An unconfigured symbol stands for one with no Pyth feeds at all.
		return ReferenceQuotes{
			Symbol: symbol,
			Equity: unavailableQuote(QuoteSourcePythEquity, QuoteReasonNoFeed),
			Token:  unavailableQuote(QuoteSourcePythCrypto, QuoteReasonNoFeed),
		}, nil
	}
	return quotes, nil
}
