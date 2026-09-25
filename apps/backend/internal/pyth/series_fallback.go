package pyth

import (
	"context"
	"errors"
	"time"
)

// FallbackSeriesSource asks each source in turn and returns the first series that
// has points in it.
//
// "Answered with nothing" and "failed" are different, and only the second is worth
// asking the next source about — except at the head of the chain, where an empty
// answer from the primary is still worth a second opinion, because a mint Jupiter
// has no candles for may well be an equity another source does know. What is never
// worth doing is asking every source for every chart: the first useful answer wins
// and the rest are not called.
type FallbackSeriesSource struct {
	sources []SeriesSource
}

// NewFallbackSeriesSource chains sources in priority order. Nil sources are dropped,
// and a chain with nothing usable in it returns nil so a caller can tell there is no
// history source at all rather than wiring one that refuses everything.
func NewFallbackSeriesSource(sources ...SeriesSource) SeriesSource {
	usable := make([]SeriesSource, 0, len(sources))
	for _, source := range sources {
		if source != nil {
			usable = append(usable, source)
		}
	}
	switch len(usable) {
	case 0:
		return nil
	case 1:
		return usable[0]
	default:
		return &FallbackSeriesSource{sources: usable}
	}
}

// Series implements SeriesSource.
func (s *FallbackSeriesSource) Series(ctx context.Context, symbol string, chartRange ChartRange, now time.Time) (AssetChartSeries, error) {
	var (
		firstEmpty AssetChartSeries
		haveEmpty  bool
		errs       []error
	)
	for _, source := range s.sources {
		series, err := source.Series(ctx, symbol, chartRange, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(series.Points) > 0 {
			return series, nil
		}
		if !haveEmpty {
			firstEmpty, haveEmpty = series, true
		}
	}
	// Somebody answered, they just had nothing. Report that rather than an error, so
	// the caller caches "no history" instead of falling through to the per-sample
	// path to learn the same thing thirty requests later.
	if haveEmpty {
		return firstEmpty, nil
	}
	return AssetChartSeries{}, errors.Join(errs...)
}
