package pricechain

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const (
	testMint   = jupiter.AAPLxMint
	testSymbol = "AAPLx"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 19, 15, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

type fakePythSource struct {
	mu    sync.Mutex
	mark  pyth.EquityMark
	err   error
	calls int
	block chan struct{}
}

func (f *fakePythSource) EquityMark(context.Context, string) (pyth.EquityMark, error) {
	f.mu.Lock()
	f.calls++
	mark, err, block := f.mark, f.err, f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return mark, err
}

func (f *fakePythSource) set(mark pyth.EquityMark, err error) {
	f.mu.Lock()
	f.mark, f.err = mark, err
	f.mu.Unlock()
}

func (f *fakePythSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeCharts struct {
	calls atomic.Int32
	err   error
}

func (f *fakeCharts) AssetMark(context.Context, string) (pyth.AssetMark, error) {
	return pyth.AssetMark{}, nil
}

func (f *fakeCharts) ChartSeries(context.Context, string, pyth.ChartRange) (pyth.AssetChartSeries, error) {
	f.calls.Add(1)
	if f.err != nil {
		return pyth.AssetChartSeries{}, f.err
	}
	return pyth.AssetChartSeries{Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 2}}}, nil
}

func entitlementError(t *testing.T) error {
	t.Helper()
	// Produce the real Hermes error type through the real client rather than forging one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/price_feeds" {
			_, _ = w.Write([]byte(`[{"id":"feed-1","market_hours":{"is_open":true}}]`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`Not entitled: feed feed-1 (no grant accepts this feed)`))
	}))
	defer srv.Close()
	pyth.ClearFeedRegistry()
	_, err := pyth.NewHermesClientWithHTTP(srv.URL, srv.Client(), "key").EquityMark(context.Background(), testSymbol)
	if !pyth.IsEntitlementError(err) {
		t.Fatalf("expected an entitlement error from Hermes 403, got %v", err)
	}
	return err
}

func testConfig(clock *fakeClock) Config {
	cfg := DefaultConfig()
	cfg.Now = clock.Now
	return cfg
}

// A $200.00/share holding: 1 whole share (1e8 atomics) bought for 200 USDC.
func testHolding() pyth.CostBasis {
	return pyth.CostBasis{Symbol: testSymbol, Mint: testMint, Units: 100_000_000, Price: 200_000_000, Amount: 100_000_000}
}

func jupiterWithPrice(micros int64, liquidity float64) jupiter.PriceClient {
	client := jupiter.NewFakePriceClient()
	jupiter.RegisterPrice(client, testMint, jupiter.TokenPrice{PriceUsdcMicros: micros, LiquidityUsd: liquidity})
	return client
}

func markOne(t *testing.T, chain *Chain) pyth.MarkedHolding {
	t.Helper()
	input, err := chain.MarkedPot(context.Background(), pyth.TreasuryRef{GroupID: "group-1", TreasuryUsdc: 5}, []pyth.CostBasis{testHolding()})
	if err != nil {
		t.Fatalf("MarkedPot: %v", err)
	}
	if len(input.Holdings) != 1 {
		t.Fatalf("expected one holding, got %d", len(input.Holdings))
	}
	if input.TreasuryUsdc != 5 {
		t.Fatalf("treasury usdc = %d, want 5", input.TreasuryUsdc)
	}
	return input.Holdings[0]
}

func TestChain_pythHealthy_usesPythAndNeverCallsJupiter(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{mark: pyth.EquityMark{PriceUsdcMicros: 210_000_000, PublishedAt: clock.Now(), MarketOpen: true}}
	jup := jupiterWithPrice(999_000_000, 500_000)
	chain := New(source, jup, nil, testConfig(clock))

	// Act
	holding := markOne(t, chain)

	// Assert
	if holding.Source != pyth.MarkSourcePyth || holding.MarkUsdc != 210_000_000 {
		t.Fatalf("got %s @ %d, want pyth @ 210000000", holding.Source, holding.MarkUsdc)
	}
	if got := jupiter.PriceCallCount(jup); got != 0 {
		t.Fatalf("jupiter called %d times while pyth was healthy", got)
	}
}

func TestChain_pyth403_usesJupiter(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{err: entitlementError(t)}
	chain := New(source, jupiterWithPrice(215_500_000, 500_000), nil, testConfig(clock))

	// Act
	holding := markOne(t, chain)

	// Assert
	if holding.Source != pyth.MarkSourceJupiter || holding.MarkUsdc != 215_500_000 {
		t.Fatalf("got %s @ %d, want jupiter @ 215500000", holding.Source, holding.MarkUsdc)
	}
	if holding.CostBasis != 200_000_000 {
		t.Fatalf("cost basis = %d, want 200000000", holding.CostBasis)
	}
}

func TestChain_bothFail_usesCostBasis(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{err: errors.New("pyth latest price: status 503")}
	jup := jupiter.NewFakePriceClient()
	jupiter.RegisterPriceError(jup, errors.New("jupiter price fetch: status 500"))
	chain := New(source, jup, nil, testConfig(clock))

	// Act
	holding := markOne(t, chain)

	// Assert
	if holding.Source != pyth.MarkSourceCostBasis || holding.MarkUsdc != 200_000_000 {
		t.Fatalf("got %s @ %d, want cost_basis @ 200000000", holding.Source, holding.MarkUsdc)
	}
}

func TestChain_noSourcesAndNoCostBasis_errors(t *testing.T) {
	// Arrange
	chain := New(nil, nil, nil, testConfig(newFakeClock()))
	holding := testHolding()
	holding.Amount = 0

	// Act
	_, err := chain.MarkedPot(context.Background(), pyth.TreasuryRef{}, []pyth.CostBasis{holding})

	// Assert
	if err == nil {
		t.Fatal("expected an error when no source and no cost basis can value the holding")
	}
}

func TestChain_stalePyth_fallsBackToJupiter(t *testing.T) {
	cases := []struct {
		name string
		mark pyth.EquityMark
		age  time.Duration
	}{
		{name: "open session older than five minutes", mark: pyth.EquityMark{PriceUsdcMicros: 210_000_000, MarketOpen: true}, age: 6 * time.Minute},
		{name: "closed session older than a long weekend", mark: pyth.EquityMark{PriceUsdcMicros: 210_000_000, AfterHours: true}, age: 97 * time.Hour},
		{name: "missing publish time", mark: pyth.EquityMark{PriceUsdcMicros: 210_000_000, MarketOpen: true}, age: -1},
		{name: "zero price", mark: pyth.EquityMark{PriceUsdcMicros: 0, MarketOpen: true}, age: time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			clock := newFakeClock()
			mark := tc.mark
			if tc.age >= 0 {
				mark.PublishedAt = clock.Now().Add(-tc.age)
			}
			chain := New(&fakePythSource{mark: mark}, jupiterWithPrice(205_000_000, 500_000), nil, testConfig(clock))

			// Act
			holding := markOne(t, chain)

			// Assert
			if holding.Source != pyth.MarkSourceJupiter {
				t.Fatalf("source = %s, want jupiter", holding.Source)
			}
		})
	}
}

func TestChain_frozenWeekendPythMark_isStillUsed(t *testing.T) {
	// Arrange — Saturday: Friday's close is two days old and the session is closed.
	clock := newFakeClock()
	mark := pyth.EquityMark{PriceUsdcMicros: 210_000_000, PublishedAt: clock.Now().Add(-48 * time.Hour), AfterHours: true}
	chain := New(&fakePythSource{mark: mark}, jupiterWithPrice(205_000_000, 500_000), nil, testConfig(clock))

	// Act
	holding := markOne(t, chain)

	// Assert
	if holding.Source != pyth.MarkSourcePyth || !holding.AfterHours {
		t.Fatalf("got %s afterHours=%v, want pyth afterHours=true", holding.Source, holding.AfterHours)
	}
}

func TestChain_malformedJupiterBody_usesCostBasis(t *testing.T) {
	bodies := map[string]string{
		"not json":       `<html>rate limited</html>`,
		"wrong shape":    `{"` + testMint + `":"334.09"}`,
		"missing price":  `{"` + testMint + `":{"liquidity":500000}}`,
		"negative price": `{"` + testMint + `":{"usdPrice":-3,"liquidity":500000}}`,
		"absurd price":   `{"` + testMint + `":{"usdPrice":1e300,"liquidity":500000}}`,
		"mint absent":    `{}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			// Arrange
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			jup := jupiter.NewHTTPPriceClientWithBaseURL(srv.URL, srv.Client(), "")
			chain := New(nil, jup, nil, testConfig(newFakeClock()))

			// Act
			holding := markOne(t, chain)

			// Assert
			if holding.Source != pyth.MarkSourceCostBasis || holding.MarkUsdc != 200_000_000 {
				t.Fatalf("got %s @ %d, want cost_basis @ 200000000", holding.Source, holding.MarkUsdc)
			}
		})
	}
}

func TestChain_jupiterSanityBounds(t *testing.T) {
	t.Run("thin liquidity is rejected", func(t *testing.T) {
		chain := New(nil, jupiterWithPrice(205_000_000, 900), nil, testConfig(newFakeClock()))
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourceCostBasis {
			t.Fatalf("source = %s, want cost_basis", holding.Source)
		}
	})

	t.Run("price far outside cost basis is rejected on a cold start", func(t *testing.T) {
		for _, micros := range []int64{1_100_000_000, 39_000_000} {
			chain := New(nil, jupiterWithPrice(micros, 500_000), nil, testConfig(newFakeClock()))
			if holding := markOne(t, chain); holding.Source != pyth.MarkSourceCostBasis {
				t.Fatalf("price %d: source = %s, want cost_basis", micros, holding.Source)
			}
		}
	})

	t.Run("jump versus last good price is rejected then accepted once the reference expires", func(t *testing.T) {
		// Arrange — Pyth sets a last good price, then goes down.
		clock := newFakeClock()
		source := &fakePythSource{mark: pyth.EquityMark{PriceUsdcMicros: 200_000_000, PublishedAt: clock.Now(), MarketOpen: true}}
		jup := jupiterWithPrice(300_000_000, 500_000)
		chain := New(source, jup, nil, testConfig(clock))
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourcePyth {
			t.Fatalf("source = %s, want pyth", holding.Source)
		}
		source.set(pyth.EquityMark{}, errors.New("pyth latest price: status 502"))

		// Act / Assert — +50% against the last good mark is refused.
		clock.Advance(time.Minute)
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourceCostBasis {
			t.Fatalf("source = %s, want cost_basis for a 50%% jump", holding.Source)
		}

		// Within bounds is accepted.
		jupiter.RegisterPrice(jup, testMint, jupiter.TokenPrice{PriceUsdcMicros: 230_000_000, LiquidityUsd: 500_000})
		clock.Advance(time.Minute)
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourceJupiter || holding.MarkUsdc != 230_000_000 {
			t.Fatalf("got %s @ %d, want jupiter @ 230000000", holding.Source, holding.MarkUsdc)
		}

		// A stale reference no longer vetoes the same large move.
		jupiter.RegisterPrice(jup, testMint, jupiter.TokenPrice{PriceUsdcMicros: 300_000_000, LiquidityUsd: 500_000})
		clock.Advance(7 * time.Hour)
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourceJupiter || holding.MarkUsdc != 300_000_000 {
			t.Fatalf("got %s @ %d, want jupiter @ 300000000", holding.Source, holding.MarkUsdc)
		}
	})
}

func TestChain_circuitBreaker_opensAndRecovers(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{err: entitlementError(t)}
	cfg := testConfig(clock)
	chain := New(source, jupiterWithPrice(205_000_000, 500_000), nil, cfg)

	// Act — many valuations across several cache windows while the feed is denied.
	for i := 0; i < 20; i++ {
		if holding := markOne(t, chain); holding.Source != pyth.MarkSourceJupiter {
			t.Fatalf("source = %s, want jupiter while pyth is denied", holding.Source)
		}
		clock.Advance(cfg.MarkTTL)
	}

	// Assert — the denied feed was asked once, not once per valuation.
	if got := source.callCount(); got != 1 {
		t.Fatalf("pyth called %d times while the breaker was open, want 1", got)
	}

	// Act — grants get accepted; the next probe after the cooldown succeeds.
	clock.Advance(cfg.EntitlementCooldown)
	source.set(pyth.EquityMark{PriceUsdcMicros: 206_000_000, PublishedAt: clock.Now(), MarketOpen: true}, nil)
	holding := markOne(t, chain)

	// Assert
	if holding.Source != pyth.MarkSourcePyth || holding.MarkUsdc != 206_000_000 {
		t.Fatalf("got %s @ %d, want pyth @ 206000000 after recovery", holding.Source, holding.MarkUsdc)
	}
	if got := source.callCount(); got != 2 {
		t.Fatalf("pyth called %d times, want 2 (initial failure + recovery probe)", got)
	}
}

func TestChain_circuitBreaker_failedProbeReopens(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{err: errors.New("pyth latest price: context deadline exceeded")}
	cfg := testConfig(clock)
	chain := New(source, jupiterWithPrice(205_000_000, 500_000), nil, cfg)

	// Act
	markOne(t, chain)
	clock.Advance(cfg.FailureCooldown)
	markOne(t, chain)
	clock.Advance(cfg.MarkTTL)
	markOne(t, chain)

	// Assert — one initial call, one failed probe, then closed off again.
	if got := source.callCount(); got != 2 {
		t.Fatalf("pyth called %d times, want 2", got)
	}
}

func TestChain_deduplicatesConcurrentAndRepeatedRequests(t *testing.T) {
	// Arrange — a home screen valuing the same holding from many goroutines at once.
	clock := newFakeClock()
	release := make(chan struct{})
	source := &fakePythSource{
		mark:  pyth.EquityMark{PriceUsdcMicros: 210_000_000, PublishedAt: clock.Now(), MarketOpen: true},
		block: release,
	}
	chain := New(source, nil, nil, testConfig(clock))

	// Act
	const callers = 25
	var wg sync.WaitGroup
	results := make([]pyth.MarkedHolding, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input, err := chain.MarkedPot(context.Background(), pyth.TreasuryRef{GroupID: "g"}, []pyth.CostBasis{testHolding()})
			if err == nil && len(input.Holdings) == 1 {
				results[i] = input.Holdings[0]
			}
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	markOne(t, chain)

	// Assert
	for i, holding := range results {
		if holding.Source != pyth.MarkSourcePyth || holding.MarkUsdc != 210_000_000 {
			t.Fatalf("caller %d got %s @ %d", i, holding.Source, holding.MarkUsdc)
		}
	}
	if got := source.callCount(); got != 1 {
		t.Fatalf("pyth called %d times for %d concurrent + 1 repeated valuation, want 1", got, callers)
	}

	// A new cache window refetches.
	clock.Advance(DefaultConfig().MarkTTL)
	markOne(t, chain)
	if got := source.callCount(); got != 2 {
		t.Fatalf("pyth called %d times after the cache window, want 2", got)
	}
}

func TestChain_firstCallerCancelled_doesNotFailSharedResolution(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{mark: pyth.EquityMark{PriceUsdcMicros: 210_000_000, PublishedAt: clock.Now(), MarketOpen: true}}
	chain := New(source, nil, nil, testConfig(clock))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Act
	input, err := chain.MarkedPot(ctx, pyth.TreasuryRef{GroupID: "g"}, []pyth.CostBasis{testHolding()})

	// Assert
	if err != nil || input.Holdings[0].Source != pyth.MarkSourcePyth {
		t.Fatalf("got %+v, %v; want a pyth mark", input.Holdings, err)
	}
}

func TestChain_holdingsFallBackIndependently(t *testing.T) {
	// Arrange — Jupiter prices AAPLx only; TSLAx has no live source.
	chain := New(nil, jupiterWithPrice(205_000_000, 500_000), nil, testConfig(newFakeClock()))
	tsla := pyth.CostBasis{Symbol: "TSLAx", Mint: jupiter.TSLAxMint, Units: 50_000_000, Price: 150_000_000, Amount: 50_000_000}

	// Act
	input, err := chain.MarkedPot(context.Background(), pyth.TreasuryRef{GroupID: "g"}, []pyth.CostBasis{testHolding(), tsla})

	// Assert
	if err != nil {
		t.Fatalf("MarkedPot: %v", err)
	}
	if input.Holdings[0].Source != pyth.MarkSourceJupiter {
		t.Fatalf("AAPLx source = %s, want jupiter", input.Holdings[0].Source)
	}
	if input.Holdings[1].Source != pyth.MarkSourceCostBasis || input.Holdings[1].MarkUsdc != 300_000_000 {
		t.Fatalf("TSLAx got %s @ %d, want cost_basis @ 300000000", input.Holdings[1].Source, input.Holdings[1].MarkUsdc)
	}
}

func TestChain_chartSeries_deniedFeedNeverReachesTheChartSampler(t *testing.T) {
	// Arrange
	clock := newFakeClock()
	source := &fakePythSource{err: entitlementError(t)}
	charts := &fakeCharts{}
	cfg := testConfig(clock)
	chain := New(source, nil, charts, cfg)

	// Act
	for i := 0; i < 10; i++ {
		series, err := chain.ChartSeries(context.Background(), testSymbol, pyth.ChartRange1D)
		if err != nil {
			t.Fatalf("ChartSeries: %v", err)
		}
		if series.EmptyReason == "" || len(series.Points) != 0 {
			t.Fatalf("expected the empty state, got %+v", series)
		}
	}

	// Assert — one entitlement probe, zero per-sample history requests.
	if got := source.callCount(); got != 1 {
		t.Fatalf("pyth probed %d times while denied, want 1", got)
	}
	if got := charts.calls.Load(); got != 0 {
		t.Fatalf("chart sampler called %d times for a denied feed, want 0", got)
	}

	// Recovery after the cooldown, then served from cache.
	clock.Advance(cfg.EntitlementCooldown)
	source.set(pyth.EquityMark{PriceUsdcMicros: 206_000_000, PublishedAt: clock.Now(), MarketOpen: true}, nil)
	for i := 0; i < 3; i++ {
		series, err := chain.ChartSeries(context.Background(), testSymbol, pyth.ChartRange1D)
		if err != nil || len(series.Points) != 1 {
			t.Fatalf("got %+v, %v; want one point", series, err)
		}
	}
	if got := charts.calls.Load(); got != 1 {
		t.Fatalf("chart sampler called %d times, want 1", got)
	}
	if got := source.callCount(); got != 2 {
		t.Fatalf("pyth probed %d times, want 2", got)
	}
}

func TestChain_chartSeries_pythOutageDoesNotBlockHistory(t *testing.T) {
	// Arrange — a 502 on the latest mark says nothing about entitlement.
	source := &fakePythSource{err: errors.New("pyth latest price: status 502")}
	charts := &fakeCharts{}
	chain := New(source, nil, charts, testConfig(newFakeClock()))

	// Act
	series, err := chain.ChartSeries(context.Background(), testSymbol, pyth.ChartRange1D)

	// Assert
	if err != nil || len(series.Points) != 1 {
		t.Fatalf("got %+v, %v; want one point", series, err)
	}
}

func TestChain_chartSeries_errorPassesThrough(t *testing.T) {
	// Arrange
	charts := &fakeCharts{err: errors.New("boom")}
	chain := New(nil, nil, charts, testConfig(newFakeClock()))

	// Act
	_, err := chain.ChartSeries(context.Background(), testSymbol, pyth.ChartRange1W)

	// Assert
	if err == nil {
		t.Fatal("expected a chart error to reach the caller")
	}
}
