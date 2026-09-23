package chainlink

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// Wednesday 23 September 2026, 18:00 UTC: inside the regular session (09:30 ET
// is 13:30 UTC in EDT), so the day window is today's and the previous session is
// Tuesday's.
var testNow = time.Date(2026, time.September, 23, 18, 0, 0, 0, time.UTC)

func testClock() func() time.Time { return func() time.Time { return testNow } }

// hourlyRounds builds one round an hour from `start` to `end`, priced from $300
// and rising a dollar an hour, which is how a feed that publishes on deviation
// and on a heartbeat actually looks: evenly enough spaced to fold, never round
// numbers.
func hourlyRounds(start, end time.Time) []evm.RoundData {
	var rounds []evm.RoundData
	price := int64(30_000_000_000)
	for at := start; !at.After(end); at = at.Add(time.Hour) {
		rounds = append(rounds, evm.RoundData{Answer: big.NewInt(price), UpdatedAt: at})
		price += 100_000_000
	}
	return rounds
}

func feedFor(t *testing.T, catalog b20.Catalog, symbol string) string {
	t.Helper()
	feed, err := catalog.Feed(context.Background(), symbol)
	if err != nil {
		t.Fatal(err)
	}
	return feed
}

// sevenWeekChain is the live shape this work was built for: a feed whose first
// round is seven weeks old.
func sevenWeekChain(t *testing.T) (b20.Catalog, *evmFake, string) {
	t.Helper()
	catalog := b20.NewPinnedCatalog()
	feed := feedFor(t, catalog, "AAPLc")
	chain := newEVMFake()
	chain.SetRoundHistory(feed, hourlyRounds(testNow.AddDate(0, 0, -49), testNow))
	return catalog, chain, feed
}

func TestChartSeries_drawsTheSessionFromRealRounds(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) < 2 {
		t.Fatalf("points = %d, want the session's rounds", len(series.Points))
	}
	// 1D is the exchange session, not a rolling 24 hours: the first point is at or
	// after 04:00 ET (08:00 UTC today), never yesterday evening.
	first := time.Unix(series.Points[0].Timestamp, 0).UTC()
	if first.Before(time.Date(2026, time.September, 23, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("first point %s is before today's pre-market open", first)
	}
	last := time.Unix(series.Points[len(series.Points)-1].Timestamp, 0).UTC()
	if last.After(testNow) {
		t.Fatalf("last point %s is in the future", last)
	}
	// The rounds are the token, per token, and the series has to say so.
	if series.Source != pyth.ChartSourceChainlink || series.Basis != pyth.PriceBasisToken || series.BasisSymbol != "AAPLc" {
		t.Fatalf("source/basis/symbol = %q/%q/%q", series.Source, series.Basis, series.BasisSymbol)
	}
	if series.Range != pyth.ChartRange1D {
		t.Fatalf("range = %q", series.Range)
	}
	if series.RegularOpen.IsZero() || series.RegularClose.IsZero() {
		t.Fatal("a session window has to carry the session it covers")
	}
	if series.EmptyReason != "" {
		t.Fatalf("empty reason on a drawn series: %q", series.EmptyReason)
	}
}

func TestChartSeries_previousCloseComesFromThePreviousSession(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if series.PreviousCloseUsdcMicros == nil {
		t.Fatal("previous close = nil, want Tuesday's last round")
	}
	// Tuesday's regular close is 20:00 UTC, and the fixture publishes on the hour,
	// so the baseline is the round struck at 20:00 UTC on 22 September.
	want := int64(0)
	for _, round := range hourlyRounds(testNow.AddDate(0, 0, -49), testNow) {
		at := round.UpdatedAt.UTC()
		if at.After(time.Date(2026, time.September, 22, 20, 0, 0, 0, time.UTC)) {
			break
		}
		want = round.Answer.Int64() / 100
	}
	if *series.PreviousCloseUsdcMicros != want {
		t.Fatalf("previous close = %d, want %d", *series.PreviousCloseUsdcMicros, want)
	}

	// And that is what the day move is measured against — inside one instrument,
	// labelled as the token's.
	change, basis, symbol := pyth.DayChangeFields(series)
	if change == nil || basis != pyth.PriceBasisToken || symbol != "AAPLc" {
		t.Fatalf("day move = %v basis %q/%q", change, basis, symbol)
	}
}

func TestChartSeries_weekAndMonthDrawTheWholeWindow(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	for _, tc := range []struct {
		chartRange pyth.ChartRange
		from       time.Time
	}{
		{pyth.ChartRange1W, testNow.AddDate(0, 0, -7)},
		{pyth.ChartRange1M, testNow.AddDate(0, -1, 0)},
	} {
		series, err := prices.ChartSeries(context.Background(), "AAPLc", tc.chartRange)
		if err != nil {
			t.Fatalf("%s: %v", tc.chartRange, err)
		}
		if len(series.Points) < 2 {
			t.Fatalf("%s: points = %d", tc.chartRange, len(series.Points))
		}
		first := time.Unix(series.Points[0].Timestamp, 0).UTC()
		if first.Before(tc.from) {
			t.Fatalf("%s: first point %s is outside the window", tc.chartRange, first)
		}
		if first.Sub(tc.from) > 2*time.Hour {
			t.Fatalf("%s: first point %s leaves a hole at the start of the window", tc.chartRange, first)
		}
		if series.PreviousCloseUsdcMicros == nil {
			t.Fatalf("%s: a window with rounds before it has a baseline", tc.chartRange)
		}
	}
}

func TestChartSeries_allIsTheWholeFeed(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRangeAll)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Unix(series.Points[0].Timestamp, 0).UTC()
	if !first.Equal(testNow.AddDate(0, 0, -49)) {
		t.Fatalf("ALL starts at %s, want the feed's first round", first)
	}
	if len(series.Points) != 49*24+1 {
		t.Fatalf("points = %d, want every round the feed has", len(series.Points))
	}
}

// The ranges the feed is too young for are refused, and the refusal names the
// day the history starts — read from the feed, never a constant. Moving the
// fixture's first round moves the message.
func TestChartSeries_rangesOlderThanTheFeedSayWhenItStarted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		firstRound time.Time
		want       string
	}{
		{testNow.AddDate(0, 0, -49), "Only on-chain since 5 Aug 2026"},
		{testNow.AddDate(0, 0, -30), "Only on-chain since 24 Aug 2026"},
	} {
		catalog := b20.NewPinnedCatalog()
		feed := feedFor(t, catalog, "AAPLc")
		chain := newEVMFake()
		chain.SetRoundHistory(feed, hourlyRounds(tc.firstRound, testNow))
		prices := NewAssetPrices(chain, catalog, testClock())

		for _, chartRange := range []pyth.ChartRange{pyth.ChartRange3M, pyth.ChartRange1Y} {
			series, err := prices.ChartSeries(context.Background(), "AAPLc", chartRange)
			if err != nil {
				t.Fatalf("%s: %v", chartRange, err)
			}
			if len(series.Points) != 0 {
				t.Fatalf("%s: %d points — seven weeks is not a %s chart", chartRange, len(series.Points), chartRange)
			}
			if series.EmptyReason != tc.want {
				t.Fatalf("%s: reason = %q, want %q", chartRange, series.EmptyReason, tc.want)
			}
			if series.Range != chartRange {
				t.Fatalf("%s: range = %q", chartRange, series.Range)
			}
		}
	}
}

// A feed young enough to have no month is refused for 1M too. The rule is about
// the data, not about which chip was tapped.
func TestChartSeries_aYoungFeedHasNoMonthEither(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed := feedFor(t, catalog, "AAPLc")
	chain := newEVMFake()
	chain.SetRoundHistory(feed, hourlyRounds(testNow.AddDate(0, 0, -3), testNow))
	prices := NewAssetPrices(chain, catalog, testClock())

	month, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1M)
	if err != nil {
		t.Fatal(err)
	}
	if len(month.Points) != 0 || !strings.HasPrefix(month.EmptyReason, "Only on-chain since") {
		t.Fatalf("1M = %d points, reason %q", len(month.Points), month.EmptyReason)
	}
	// The session it does have still draws.
	day, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Points) < 2 {
		t.Fatalf("1D = %d points, want the session a three-day-old feed does have", len(day.Points))
	}
}

// A read cut short by the chain is a failure, not a shorter chart: three weeks
// drawn under a "1M" chip is the same lie as seven weeks under "1Y".
func TestSeriesFromRounds_truncatedReadIsAnErrorNotAShortChart(t *testing.T) {
	t.Parallel()
	window := chartWindowFor(pyth.ChartRange1M, testNow)
	history := evm.RoundHistory{
		Rounds:       roundsToEVM(hourlyRounds(testNow.AddDate(0, 0, -20), testNow)),
		FirstRoundAt: testNow.AddDate(0, 0, -49),
		Truncated:    true,
	}
	_, err := seriesFromRounds("AAPLc", pyth.ChartRange1M, window, history, testNow)
	if !errors.Is(err, ErrHistoryShort) {
		t.Fatalf("err = %v, want ErrHistoryShort", err)
	}
}

func TestChartSeries_rpcFailureIsAnErrorNotAnEmptyChart(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed := feedFor(t, catalog, "AAPLc")
	chain := newEVMFake()
	chain.SetRoundError(feed, fmt.Errorf("rpc: over rate limit"))
	prices := NewAssetPrices(chain, catalog, testClock())

	_, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err == nil {
		t.Fatal("a rate-limited chain must not read as 'no price history'")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("err = %v, want the reason to survive", err)
	}
}

func TestChartSeries_skipsRoundsWithNoPriceOrNoTime(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed := feedFor(t, catalog, "AAPLc")
	rounds := hourlyRounds(testNow.AddDate(0, 0, -49), testNow)
	rounds[len(rounds)-3].Answer = big.NewInt(0)
	rounds[len(rounds)-2].UpdatedAt = time.Unix(0, 0)
	chain := newEVMFake()
	chain.SetRoundHistory(feed, rounds)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range series.Points {
		if point.PriceUsdcMicros <= 0 {
			t.Fatalf("drew an unpriced round: %+v", point)
		}
		at := time.Unix(point.Timestamp, 0).UTC()
		if at.Year() < 2020 {
			t.Fatalf("drew a round dated %s", at)
		}
	}
}

func TestChartSeries_oneRoundInTheWindowIsNotACurve(t *testing.T) {
	t.Parallel()
	catalog := b20.NewPinnedCatalog()
	feed := feedFor(t, catalog, "AAPLc")
	rounds := hourlyRounds(testNow.AddDate(0, 0, -49), testNow.AddDate(0, 0, -1).Add(-2*time.Hour))
	rounds = append(rounds, evm.RoundData{Answer: big.NewInt(33_000_000_000), UpdatedAt: testNow.Add(-time.Hour)})
	chain := newEVMFake()
	chain.SetRoundHistory(feed, rounds)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 0 || series.EmptyReason != pyth.EmptyReasonNoHistory {
		t.Fatalf("series = %+v, want empty: one dot is not a line", series)
	}
}

// The chart cache is what keeps a screen that opens six ranges from being six
// walks of the chain.
func TestChartSeries_isCachedPerSymbolAndRange(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	if _, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1M); err != nil {
		t.Fatal(err)
	}
	cold := chain.reads.Load()
	if cold == 0 {
		t.Fatal("the first read has to reach the chain")
	}
	for i := 0; i < 5; i++ {
		if _, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1M); err != nil {
			t.Fatal(err)
		}
	}
	if warm := chain.reads.Load() - cold; warm != 0 {
		t.Fatalf("five warm reads cost %d chain reads, want 0", warm)
	}
	// A different range is a different answer and does reach the chain.
	if _, err := prices.ChartSeries(context.Background(), "AAPLc", pyth.ChartRange1D); err != nil {
		t.Fatal(err)
	}
	if chain.reads.Load() == cold {
		t.Fatal("a range that was never read must not be served from another range's entry")
	}
}

func TestChartSeries_unknownSymbolIsEmptyNotAnError(t *testing.T) {
	t.Parallel()
	catalog, chain, _ := sevenWeekChain(t)
	prices := NewAssetPrices(chain, catalog, testClock())

	series, err := prices.ChartSeries(context.Background(), "NOTAREALTICKER", pyth.ChartRange1D)
	if err != nil {
		t.Fatalf("err = %v: a symbol with no feed is not a chain failure", err)
	}
	if len(series.Points) != 0 || series.EmptyReason != pyth.EmptyReasonNoHistory {
		t.Fatalf("series = %+v", series)
	}
}

func roundsToEVM(rounds []evm.RoundData) []evm.RoundData {
	out := make([]evm.RoundData, 0, len(rounds))
	for _, round := range rounds {
		round.UpdatedAt = round.UpdatedAt.UTC()
		out = append(out, round)
	}
	return out
}

// evmFake counts the history reads a chart costs, so the cache is asserted by
// what it prevents rather than by its own internals.
type evmFake struct {
	evm.Client
	reads atomic.Int64
}

func newEVMFake() *evmFake {
	return &evmFake{Client: evm.NewFakeClient()}
}

func (f *evmFake) SetRoundHistory(feed string, rounds []evm.RoundData) {
	f.Client.(interface {
		SetRoundHistory(string, []evm.RoundData)
	}).SetRoundHistory(feed, rounds)
}

func (f *evmFake) SetRoundError(feed string, err error) {
	f.Client.(interface {
		SetRoundError(string, error)
	}).SetRoundError(feed, err)
}

func (f *evmFake) ChainlinkRoundsSince(ctx context.Context, feed string, since time.Time, maxRounds int) (evm.RoundHistory, error) {
	f.reads.Add(1)
	return f.Client.ChainlinkRoundsSince(ctx, feed, since, maxRounds)
}
