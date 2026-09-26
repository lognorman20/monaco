package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// PollerPriceAlerts is the alert poller's name in metrics, /health and alerts.
const PollerPriceAlerts = "price_alerts"

// DefaultAlertPollInterval is how often price alerts are checked. A minute is as fresh as a
// member expects "tell me when" to be, and it prices every watched symbol in one batched
// Jupiter call per 50 symbols.
const DefaultAlertPollInterval = time.Minute

// alertTickBudget bounds one pass well inside the interval, so passes never stack.
const alertTickBudget = 45 * time.Second

// notifyBudget bounds one notifier call, so a slow delivery cannot eat the pass.
const notifyBudget = 10 * time.Second

// PriceAlertStore is the slice of the store the alert poller needs. *postgres.Store
// implements it.
type PriceAlertStore interface {
	ActivePriceAlertSymbols(ctx context.Context) ([]string, error)
	TriggerPriceAlerts(ctx context.Context, symbol string, markUsdcMicros int64, at time.Time) ([]postgres.PriceAlertRow, error)
}

// AlertPoller fires price alerts. Each pass reads the symbols that have an active alert,
// prices them the way the Stocks tab prices a row, and for each symbol fires every alert
// whose line the mark has reached. Firing is one statement that also deactivates the alert,
// so an alert fires exactly once: the next pass no longer sees it, and neither does a second
// API process running its own poller. Each fired alert is handed to the notifier once.
type AlertPoller struct {
	store    PriceAlertStore
	marks    app.AlertMarkSource
	notifier app.AlertNotifier
	clock    Clock
}

// NewAlertPoller wires the poller. A nil notifier logs; a nil clock is the wall clock.
func NewAlertPoller(store PriceAlertStore, marks app.AlertMarkSource, notifier app.AlertNotifier, clock Clock) *AlertPoller {
	if notifier == nil {
		notifier = app.LogAlertNotifier{}
	}
	if clock == nil {
		clock = systemClock{}
	}
	return &AlertPoller{store: store, marks: marks, notifier: notifier, clock: clock}
}

// Tick runs one pass and reports how many alerts fired. An error means the pass could not
// read its symbols or its marks; nothing fired, and the next pass tries again.
func (p *AlertPoller) Tick(ctx context.Context) (int, error) {
	if p == nil || p.store == nil || p.marks == nil {
		return 0, nil
	}
	symbols, err := p.store.ActivePriceAlertSymbols(ctx)
	if err != nil {
		return 0, err
	}
	if len(symbols) == 0 {
		return 0, nil
	}
	marks, err := p.marks.Marks(ctx, symbols)
	if err != nil {
		return 0, err
	}

	now := p.clock.Now().UTC()
	fired := 0
	for _, symbol := range symbols {
		mark, ok := marks[strings.ToUpper(strings.TrimSpace(symbol))]
		if !ok || mark.PriceUsdcMicros <= 0 {
			// No price this pass: the alert waits. Never fire on a missing number.
			continue
		}
		triggered, err := p.store.TriggerPriceAlerts(ctx, symbol, mark.PriceUsdcMicros, now)
		if err != nil {
			slog.ErrorContext(ctx, "price alerts trigger failed", "symbol", symbol, "err", err)
			continue
		}
		for _, alert := range triggered {
			fired++
			p.notify(ctx, alert, mark)
		}
	}
	return fired, nil
}

func (p *AlertPoller) notify(ctx context.Context, alert postgres.PriceAlertRow, mark app.AlertMark) {
	event := app.AlertEvent{
		AlertID:         alert.ID,
		Symbol:          alert.Symbol,
		Name:            mark.Name,
		Direction:       app.AlertDirection(alert.Direction),
		Threshold:       alert.PriceUsdcMicros,
		PriceUsdcMicros: mark.PriceUsdcMicros,
	}
	if alert.TriggeredAt != nil {
		event.TriggeredAt = alert.TriggeredAt.UTC()
	}
	if alert.TriggeredPriceUsdcMicros != nil {
		event.PriceUsdcMicros = *alert.TriggeredPriceUsdcMicros
	}
	notifyCtx, cancel := context.WithTimeout(ctx, notifyBudget)
	defer cancel()
	if err := p.notifier.Notify(notifyCtx, alert.UserID, event); err != nil {
		// At most once, by design: see app.AlertNotifier.
		slog.ErrorContext(ctx, "price alert notify failed",
			"alert_id", alert.ID,
			"user_id", alert.UserID,
			"symbol", alert.Symbol,
			"err", err,
		)
	}
}

// RunAlertPoller checks alerts on every interval until ctx ends.
func RunAlertPoller(ctx context.Context, poller *AlertPoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultAlertPollInterval
	}
	slog.Info("price alert poller started", "interval", interval)
	telemetry.RegisterPoller(PollerPriceAlerts, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer slog.Info("price alert poller stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, alertTickBudget)
			telemetry.GuardTick(ctx, PollerPriceAlerts, func() error {
				fired, err := poller.Tick(tickCtx)
				if err != nil && ctx.Err() == nil {
					slog.Warn("price alert tick failed", "err", err)
					return err
				}
				if fired > 0 {
					slog.Info("price alerts fired", "count", fired)
				}
				return nil
			})
			cancel()
		}
	}
}
