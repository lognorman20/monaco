package pyth

import (
	"errors"
	"net/http"
)

// ErrFeedNotFound is Hermes having no feed matching a query. Unlike an outage this
// is a permanent answer for that symbol, so callers report it as unavailable rather
// than retrying.
var ErrFeedNotFound = errors.New("pyth feed not found")

func isFeedNotFound(err error) bool { return errors.Is(err, ErrFeedNotFound) }

// RequestError is a non-200 response from a Pyth host. Callers inspect Status to
// tell an entitlement denial (401/403) from an outage (5xx) or a rate limit (429).
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

// confToUSDCMicros converts a Pyth confidence interval, returning 0 when Hermes
// omitted it or published something unusable. A bad conf is not a reason to lose
// the price it came with; it only means the certainty cell is left out.
func confToUSDCMicros(conf string, expo int32) int64 {
	if conf == "" {
		return 0
	}
	micros, err := priceToUSDCMicros(conf, expo)
	if err != nil || micros < 0 {
		return 0
	}
	return micros
}
