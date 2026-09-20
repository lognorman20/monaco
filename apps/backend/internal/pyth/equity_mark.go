package pyth

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// EquityMark fetches one symbol's latest Hermes mark with the metadata the price
// chain needs to judge freshness.
func (c *HermesClient) EquityMark(ctx context.Context, symbol string) (EquityMark, error) {
	feedID, isOpen, err := c.resolveFeedSession(ctx, symbol)
	if err != nil {
		return EquityMark{}, err
	}

	latest, err := c.fetchLatestPrice(ctx, feedID)
	if err != nil {
		return EquityMark{}, err
	}

	markUsdc, err := priceToUSDCMicros(latest.Price.Price, latest.Price.Expo)
	if err != nil {
		return EquityMark{}, err
	}

	// After-hours when cash equity session closed (Hermes market_hours.is_open)
	// or Hermes serves a frozen mark (publish_time == prev_publish_time).
	afterHours := !isOpen || isFrozenEquityMark(latest.Price.PublishTime, latest.Metadata.PrevPublishTime)

	var publishedAt time.Time
	if latest.Price.PublishTime > 0 {
		publishedAt = time.Unix(latest.Price.PublishTime, 0).UTC()
	}
	return EquityMark{
		PriceUsdcMicros: markUsdc,
		PublishedAt:     publishedAt,
		MarketOpen:      isOpen,
		AfterHours:      afterHours,
	}, nil
}

// RequestError is a non-200 Hermes response. Callers inspect Status to tell an
// entitlement denial (403) from an outage (5xx).
type RequestError struct {
	Status  int
	message string
}

func (e *RequestError) Error() string { return e.message }

// IsEntitlementError reports whether err is Hermes refusing the API key for a feed.
// That does not heal on retry: someone has to accept grants in Pyth Terminal.
func IsEntitlementError(err error) bool {
	var reqErr *RequestError
	if !errors.As(err, &reqErr) {
		return false
	}
	return reqErr.Status == http.StatusForbidden || reqErr.Status == http.StatusUnauthorized
}
