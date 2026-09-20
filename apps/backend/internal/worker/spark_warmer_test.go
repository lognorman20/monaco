package worker

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// countingChartClient records which symbols were asked for the day series.
type countingChartClient struct {
	mu      sync.Mutex
	asked   []string
	failAll bool
}

func (c *countingChartClient) AssetMark(context.Context, string) (pyth.AssetMark, error) {
	return pyth.AssetMark{}, errors.New("not used")
}

func (c *countingChartClient) ChartSeries(_ context.Context, symbol string, chartRange pyth.ChartRange) (pyth.AssetChartSeries, error) {
	c.mu.Lock()
	c.asked = append(c.asked, string(chartRange)+":"+symbol)
	failAll := c.failAll
	c.mu.Unlock()
	if failAll {
		return pyth.AssetChartSeries{}, errors.New("hermes unavailable")
	}
	return pyth.AssetChartSeries{}, nil
}

func (c *countingChartClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.asked)
}

func TestNewSparkWarmer_withoutDependenciesThereIsNothingToStart(t *testing.T) {
	t.Parallel()
	if NewSparkWarmer(nil, &countingChartClient{}, 10) != nil {
		t.Fatal("a warmer with no catalogue should be nil")
	}
	if NewSparkWarmer(xstocks.NewFakeCatalogSearcher(), nil, 10) != nil {
		t.Fatal("a warmer with no price client should be nil")
	}
}

func TestSparkWarmer_warmsTheDayRangeForEachPinnedSymbol(t *testing.T) {
	t.Parallel()
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: "mint-aapl"})
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "TSLAx", Name: "Tesla", SolanaMint: "mint-tsla"})
	chart := &countingChartClient{}

	warmer := NewSparkWarmer(catalog, chart, 10)
	if err := warmer.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if chart.count() == 0 {
		t.Fatal("the warmer asked for nothing")
	}
	chart.mu.Lock()
	defer chart.mu.Unlock()
	for _, asked := range chart.asked {
		if got := asked[:len(pyth.ChartRange1D)]; got != string(pyth.ChartRange1D) {
			t.Fatalf("warmed %q, want only the 1D range", asked)
		}
	}
}

func TestSparkWarmer_anUpstreamFailureIsNotATickFailure(t *testing.T) {
	t.Parallel()
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: "mint-aapl"})
	chart := &countingChartClient{failAll: true}

	// Nobody is waiting on this pass. A symbol that did not warm is simply not
	// warm, and the next tick tries again.
	warmer := NewSparkWarmer(catalog, chart, 10)
	if err := warmer.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func TestSparkWarmer_aCatalogueFailureIsReported(t *testing.T) {
	t.Parallel()
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogSearchError(catalog, errors.New("catalogue down"))

	warmer := NewSparkWarmer(catalog, &countingChartClient{}, 10)
	if err := warmer.Tick(context.Background()); err == nil {
		t.Fatal("a catalogue that cannot list the pinned symbols is worth logging")
	}
}

func TestSparkWarmer_aCancelledContextStopsTheTick(t *testing.T) {
	t.Parallel()
	catalog := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalog, xstocks.CatalogAsset{Symbol: "AAPLx", Name: "Apple", SolanaMint: "mint-aapl"})
	chart := &countingChartClient{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	warmer := NewSparkWarmer(catalog, chart, 10)
	// The catalogue read fails on a dead context; either way the tick returns
	// rather than hanging, which is what RunSparkWarmer relies on at shutdown.
	_ = warmer.Tick(ctx)
}

func TestRunSparkWarmer_nilWarmerReturnsImmediately(t *testing.T) {
	t.Parallel()
	RunSparkWarmer(context.Background(), nil, DefaultSparkWarmInterval)
}
