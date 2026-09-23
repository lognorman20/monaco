package worker

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// countingWarmer records which symbols were refreshed.
type countingWarmer struct {
	mu      sync.Mutex
	asked   []string
	failAll bool
	delay   time.Duration
}

func (c *countingWarmer) WarmDaySeries(ctx context.Context, symbol string) bool {
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return false
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, symbol)
	return !c.failAll
}

func (c *countingWarmer) symbols() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]string(nil), c.asked...)
	sort.Strings(out)
	return out
}

// failingPopular is a catalog that cannot list its popular symbols.
type failingPopular struct{ b20.Catalog }

func (failingPopular) Popular(context.Context) ([]b20.Asset, error) {
	return nil, errors.New("catalog down")
}

func TestNewSparkWarmer_withoutDependenciesThereIsNothingToStart(t *testing.T) {
	t.Parallel()
	if NewSparkWarmer(nil, &countingWarmer{}) != nil {
		t.Fatal("a warmer with no catalog should be nil")
	}
	if NewSparkWarmer(b20.NewPinnedCatalog(), nil) != nil {
		t.Fatal("a warmer with no series client should be nil")
	}
}

func TestSparkWarmer_refreshesEveryPopularSymbol(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	popular, _ := catalog.Popular(context.Background())
	series := &countingWarmer{}

	warmed, attempted, err := NewSparkWarmer(catalog, series).Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if attempted != len(popular) || warmed != len(popular) {
		t.Fatalf("warmed %d of %d, want all %d", warmed, attempted, len(popular))
	}
	want := make([]string, 0, len(popular))
	for _, asset := range popular {
		want = append(want, asset.Symbol)
	}
	sort.Strings(want)
	got := series.symbols()
	if len(got) != len(want) {
		t.Fatalf("warmed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warmed %v, want %v", got, want)
		}
	}
}

func TestSparkWarmer_anUpstreamFailureIsNotATickFailure(t *testing.T) {
	t.Parallel()
	// Nobody is waiting on this pass. A symbol that did not warm is simply not
	// warm, and the next tick tries again.
	warmed, attempted, err := NewSparkWarmer(b20.NewPinnedCatalog(), &countingWarmer{failAll: true}).Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if warmed != 0 || attempted == 0 {
		t.Fatalf("warmed %d of %d, want 0 of some", warmed, attempted)
	}
}

func TestSparkWarmer_aCatalogFailureIsReported(t *testing.T) {
	t.Parallel()
	_, _, err := NewSparkWarmer(failingPopular{b20.NewPinnedCatalog()}, &countingWarmer{}).Tick(context.Background())
	if err == nil {
		t.Fatal("a catalog that cannot list the popular symbols is worth logging")
	}
}

func TestSparkWarmer_aCancelledContextEndsTheTick(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		_, _, _ = NewSparkWarmer(b20.NewPinnedCatalog(), &countingWarmer{delay: time.Minute}).Tick(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled tick did not return; shutdown would hang on it")
	}
}

func TestRunSparkWarmer_nilWarmerReturnsImmediately(t *testing.T) {
	t.Parallel()
	RunSparkWarmer(context.Background(), nil, DefaultSparkWarmInterval)
}

func TestRunSparkWarmer_warmsAtBootAndStopsWithItsContext(t *testing.T) {
	t.Parallel()
	series := &countingWarmer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunSparkWarmer(ctx, NewSparkWarmer(b20.NewPinnedCatalog(), series), time.Hour)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(series.symbols()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(series.symbols()) == 0 {
		t.Fatal("the first pass did not run at boot")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the warmer did not stop with its context")
	}
}

func TestDefaultSparkWarmInterval_refreshesInsideTheDayCacheLifetime(t *testing.T) {
	t.Parallel()
	// A refresh every interval replaces the entry before it expires only if the
	// interval is shorter than the entry's lifetime.
	if DefaultSparkWarmInterval >= pyth.ChartCacheDayTTL {
		t.Fatalf("interval %s lets a %s 1D entry lapse between ticks", DefaultSparkWarmInterval, pyth.ChartCacheDayTTL)
	}
	if sparkWarmTickBudget >= DefaultSparkWarmInterval {
		t.Fatalf("tick budget %s can outrun the interval %s and stack passes", sparkWarmTickBudget, DefaultSparkWarmInterval)
	}
}
