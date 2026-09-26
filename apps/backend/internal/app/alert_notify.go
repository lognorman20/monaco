package app

import (
	"context"
	"errors"
	"fmt"
)

// NotifyPriceAlert is the inbox kind for a price alert that fired. It sits in the results
// category: it is the market answering a question the member asked, the way a fill answers a
// vote, and the "Results and fills" switch is the one that turns market news off.
const NotifyPriceAlert = "price_alert"

// PriceAlertNotifier delivers a fired price alert the way every other event reaches a member:
// an inbox row and a push, through the Notifier. It is the AlertNotifier the alert poller runs
// with once notifications are wired; until then the poller logs.
type PriceAlertNotifier struct {
	notifier *Notifier
}

// NewPriceAlertNotifier wraps the app's Notifier. A nil Notifier falls back to the log.
func NewPriceAlertNotifier(notifier *Notifier) *PriceAlertNotifier {
	return &PriceAlertNotifier{notifier: notifier}
}

// Notify implements AlertNotifier. The alert is already inactive, so a write that fails is
// reported and not retried; the poller's contract is at most once.
func (p *PriceAlertNotifier) Notify(ctx context.Context, userID string, event AlertEvent) error {
	if p == nil || p.notifier == nil {
		return LogAlertNotifier{}.Notify(ctx, userID, event)
	}
	title, body := priceAlertCopy(event)
	written := p.notifier.Notify(ctx, []string{userID}, Notification{
		Kind:   NotifyPriceAlert,
		Title:  title,
		Body:   body,
		Symbol: event.Symbol,
	})
	if written == 0 {
		return errors.New("price alert notification was not written")
	}
	return nil
}

// priceAlertCopy: "Alphabet is above $360" / "It is at $361.20 now. You asked to hear about this."
func priceAlertCopy(event AlertEvent) (string, string) {
	side := "above"
	if event.Direction == AlertBelow {
		side = "below"
	}
	name := event.Name
	if name == "" {
		name = notifyAssetName(event.Symbol)
	}
	title := fmt.Sprintf("%s is %s %s", name, side, formatNotifyUSD(event.Threshold))
	body := fmt.Sprintf("It is at %s now. You asked to hear about this.", formatNotifyUSD(event.PriceUsdcMicros))
	return title, body
}
