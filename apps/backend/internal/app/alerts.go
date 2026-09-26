package app

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// AlertDirection is which side of its line a price alert waits for.
type AlertDirection string

const (
	// AlertAbove fires once the mark is at or above the line.
	AlertAbove AlertDirection = postgres.PriceAlertAbove
	// AlertBelow fires once the mark is at or below the line.
	AlertBelow AlertDirection = postgres.PriceAlertBelow
)

// ParseAlertDirection reads "above" or "below", in any casing.
func ParseAlertDirection(raw string) (AlertDirection, bool) {
	switch AlertDirection(strings.ToLower(strings.TrimSpace(raw))) {
	case AlertAbove:
		return AlertAbove, true
	case AlertBelow:
		return AlertBelow, true
	default:
		return "", false
	}
}

// Reached reports whether a mark has reached the line: at or above it for AlertAbove, at or
// below it for AlertBelow. It is the rule the store's TriggerPriceAlerts applies in SQL, and
// the rule a new alert is checked against so none is created already met.
func (d AlertDirection) Reached(markUsdcMicros, lineUsdcMicros int64) bool {
	switch d {
	case AlertAbove:
		return markUsdcMicros >= lineUsdcMicros
	case AlertBelow:
		return markUsdcMicros <= lineUsdcMicros
	default:
		return false
	}
}

// AlertEvent is one price alert that has just fired. The alert is already inactive when the
// event is sent: firing and deactivating are one statement, so an alert produces exactly one
// event however many pollers run.
type AlertEvent struct {
	// AlertID names the alert, for de-duplicating a delivery or opening it from a push.
	AlertID string
	// Symbol is the catalogue's symbol ("GOOGLx"); Name is its display name ("Alphabet").
	Symbol string
	Name   string
	// Direction and Threshold are the line the member set: "above $360.00".
	Direction AlertDirection
	Threshold int64
	// PriceUsdcMicros is the mark that reached the line, priced the way the Stocks tab
	// prices a row.
	PriceUsdcMicros int64
	// TriggeredAt is when the poller fired it, UTC.
	TriggeredAt time.Time
}

// AlertNotifier tells a member that one of their price alerts fired.
//
// It is called once per fired alert, after the alert has been marked triggered, from the
// alert poller's goroutine. Delivery is therefore at most once: a Notify that fails is
// logged and not retried, because retrying would need the alert to stay active, and an
// active alert on a price that has already crossed would fire again on every tick.
// Implementations should return promptly (the poller passes a bounded context) and must be
// safe to call from one goroutine at a time.
//
// The default is LogAlertNotifier. The notifications lane replaces it with push delivery.
type AlertNotifier interface {
	Notify(ctx context.Context, userID string, event AlertEvent) error
}

// LogAlertNotifier records fired alerts in the log and delivers nothing else. It is what
// runs until a real notifier is wired.
type LogAlertNotifier struct{}

// Notify logs the event.
func (LogAlertNotifier) Notify(ctx context.Context, userID string, event AlertEvent) error {
	slog.InfoContext(ctx, "price alert fired",
		"alert_id", event.AlertID,
		"user_id", userID,
		"symbol", event.Symbol,
		"direction", string(event.Direction),
		"threshold_usdc_micros", event.Threshold,
		"mark_usdc_micros", event.PriceUsdcMicros,
		"triggered_at", event.TriggeredAt.UTC().Format(time.RFC3339),
	)
	return nil
}
