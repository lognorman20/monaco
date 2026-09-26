package app

import (
	"context"
	"testing"
	"time"
)

// A fired alert lands in the member's inbox and on their devices, under the results
// category, with the stock's symbol so the row draws its mark.
func TestPriceAlertNotifier_writesInboxRowAndPush(t *testing.T) {
	c := newNotifyCabal(t, "Ada")
	notifier := NewPriceAlertNotifier(c.notifier)
	event := AlertEvent{
		AlertID:         "alert-1",
		Symbol:          "GOOGLx",
		Name:            "Alphabet",
		Direction:       AlertAbove,
		Threshold:       360_000_000,
		PriceUsdcMicros: 361_200_000,
		TriggeredAt:     c.now,
	}
	if err := notifier.Notify(context.Background(), c.members[0], event); err != nil {
		t.Fatalf("notify: %v", err)
	}
	rows := c.inbox(t, 0)
	if len(rows) != 1 {
		t.Fatalf("inbox rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != NotifyPriceAlert {
		t.Fatalf("row kind = %s, want %s", row.Kind, NotifyPriceAlert)
	}
	if NotificationCategory(row.Kind) != NotifyCategoryResults {
		t.Fatalf("category = %s, want %s", NotificationCategory(row.Kind), NotifyCategoryResults)
	}
	if row.Title != "Alphabet is above $360" {
		t.Fatalf("title = %q", row.Title)
	}
	if row.Body != "It is at $361.20 now. You asked to hear about this." {
		t.Fatalf("body = %q", row.Body)
	}
	if row.Symbol.String != "GOOGLx" {
		t.Fatalf("symbol = %q, want GOOGLx", row.Symbol.String)
	}
	pushes := c.push.forUser(c.members[0])
	if len(pushes) != 1 || pushes[0].Kind != NotifyPriceAlert {
		t.Fatalf("pushes = %+v, want one price_alert", pushes)
	}
}

// A member who turned results off gets neither the row nor the push, and the notifier says so
// rather than pretending it delivered.
func TestPriceAlertNotifier_respectsTheResultsSwitch(t *testing.T) {
	c := newNotifyCabal(t, "Ada")
	setNotificationPreference(t, c, 0, NotifyCategoryResults, false)
	notifier := NewPriceAlertNotifier(c.notifier)
	err := notifier.Notify(context.Background(), c.members[0], AlertEvent{
		Symbol: "GOOGLx", Name: "Alphabet", Direction: AlertBelow, Threshold: 330_000_000,
		PriceUsdcMicros: 329_000_000, TriggeredAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatalf("expected an error when nothing was written")
	}
	if rows := c.inbox(t, 0); len(rows) != 0 {
		t.Fatalf("inbox rows = %d, want 0", len(rows))
	}
}

// Without a Notifier the adapter is the log, so the poller never has a nil notifier.
func TestPriceAlertNotifier_nilNotifierLogs(t *testing.T) {
	var notifier *PriceAlertNotifier
	if err := notifier.Notify(context.Background(), "u1", AlertEvent{Symbol: "AAPLx", Direction: AlertAbove}); err != nil {
		t.Fatalf("nil notifier: %v", err)
	}
}

func TestPriceAlertCopy(t *testing.T) {
	title, body := priceAlertCopy(AlertEvent{Symbol: "TSLAx", Direction: AlertBelow, Threshold: 400_000_000, PriceUsdcMicros: 398_500_000})
	if title != "Tesla is below $400" {
		t.Fatalf("title = %q", title)
	}
	if body != "It is at $398.50 now. You asked to hear about this." {
		t.Fatalf("body = %q", body)
	}
}
