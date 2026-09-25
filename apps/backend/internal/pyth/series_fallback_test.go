package pyth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type recordingSource struct {
	name   string
	series AssetChartSeries
	err    error
	calls  *[]string
}

func (s recordingSource) Series(context.Context, string, ChartRange, time.Time) (AssetChartSeries, error) {
	*s.calls = append(*s.calls, s.name)
	return s.series, s.err
}

func seriesWithPoints(prices ...int64) AssetChartSeries {
	points := make([]ChartPoint, 0, len(prices))
	for i, price := range prices {
		points = append(points, ChartPoint{Timestamp: int64(1_700_000_000 + i*60), PriceUsdcMicros: price})
	}
	return AssetChartSeries{Points: points}
}

func TestFallbackSeriesSource_firstUsefulAnswerWins(t *testing.T) {
	var calls []string
	source := NewFallbackSeriesSource(
		recordingSource{name: "primary", series: seriesWithPoints(1_000_000), calls: &calls},
		recordingSource{name: "backup", series: seriesWithPoints(2_000_000), calls: &calls},
	)

	series, err := source.Series(context.Background(), "AAPLx", ChartRange1D, time.Now())
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 1 || series.Points[0].PriceUsdcMicros != 1_000_000 {
		t.Fatalf("got the wrong series: %+v", series.Points)
	}
	if len(calls) != 1 || calls[0] != "primary" {
		t.Errorf("calls = %v, want only the primary — a working source must not cost a second request", calls)
	}
}

func TestFallbackSeriesSource_afailureFallsThrough(t *testing.T) {
	var calls []string
	source := NewFallbackSeriesSource(
		recordingSource{name: "primary", err: errors.New("429"), calls: &calls},
		recordingSource{name: "backup", series: seriesWithPoints(2_000_000), calls: &calls},
	)

	series, err := source.Series(context.Background(), "AAPLx", ChartRange1D, time.Now())
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 1 || series.Points[0].PriceUsdcMicros != 2_000_000 {
		t.Fatalf("got the wrong series: %+v", series.Points)
	}
	if len(calls) != 2 {
		t.Errorf("calls = %v, want both", calls)
	}
}

// One source having no candles for a mint does not mean nobody does, so an empty
// answer at the head of the chain is still worth a second opinion.
func TestFallbackSeriesSource_anEmptyAnswerStillTriesTheNextSource(t *testing.T) {
	var calls []string
	source := NewFallbackSeriesSource(
		recordingSource{name: "primary", series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}, calls: &calls},
		recordingSource{name: "backup", series: seriesWithPoints(2_000_000), calls: &calls},
	)

	series, err := source.Series(context.Background(), "AAPLx", ChartRange1D, time.Now())
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("points = %d, want the backup's one point", len(series.Points))
	}
	if len(calls) != 2 {
		t.Errorf("calls = %v, want both", calls)
	}
}

// Everybody answered and nobody had anything. That is an answer: it must come back
// as an empty series, not an error, so the caller caches it instead of falling
// through to the thirty-request sampler to learn the same thing.
func TestFallbackSeriesSource_allEmptyIsAnEmptySeriesNotAnError(t *testing.T) {
	var calls []string
	source := NewFallbackSeriesSource(
		recordingSource{name: "primary", series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}, calls: &calls},
		recordingSource{name: "backup", series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}, calls: &calls},
	)

	series, err := source.Series(context.Background(), "NEWx", ChartRange1D, time.Now())
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if series.EmptyReason != EmptyReasonNoHistory {
		t.Errorf("emptyReason = %q, want %q", series.EmptyReason, EmptyReasonNoHistory)
	}
}

func TestFallbackSeriesSource_everySourceFailingIsAnError(t *testing.T) {
	var calls []string
	primary := errors.New("jupiter down")
	backup := errors.New("benchmarks down")
	source := NewFallbackSeriesSource(
		recordingSource{name: "primary", err: primary, calls: &calls},
		recordingSource{name: "backup", err: backup, calls: &calls},
	)

	_, err := source.Series(context.Background(), "AAPLx", ChartRange1D, time.Now())
	if err == nil {
		t.Fatal("want an error so the breaker trips and the sampler gets its turn")
	}
	// Both causes survive, so a log line says which upstreams were down rather than
	// naming only the last one tried.
	if !errors.Is(err, primary) || !errors.Is(err, backup) {
		t.Errorf("err = %v, want both causes joined", err)
	}
}

func TestNewFallbackSeriesSource_dropsNilsAndUnwrapsASingleSource(t *testing.T) {
	var calls []string
	only := recordingSource{name: "only", series: seriesWithPoints(1), calls: &calls}

	if got := NewFallbackSeriesSource(nil, nil); got != nil {
		t.Errorf("a chain of nothing = %v, want nil so a caller can tell there is no source", got)
	}
	// One usable source needs no wrapper around it. (recordingSource holds a slice,
	// so it is not comparable; the concrete type is what the assertion is about.)
	if _, wrapped := NewFallbackSeriesSource(nil, only, nil).(*FallbackSeriesSource); wrapped {
		t.Error("a chain of one was wrapped; it should be the source itself")
	}
}
