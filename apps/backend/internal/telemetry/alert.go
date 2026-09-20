package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Severity ranks an alert. Critical means money is stuck or the API is about to stop
// working; warning means degraded but self-healing.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

const (
	// DefaultAlertCooldown is how long a repeat of the same alert key is suppressed. The
	// recovery poller re-reports a wedged cash out every tick; a channel needs it once.
	DefaultAlertCooldown = 15 * time.Minute

	alertQueueSize     = 64
	alertSendTimeout   = 5 * time.Second
	alertDrainTimeout  = 3 * time.Second
	maxAlertDetailSize = 500
)

// AlertEvent is something a person has to look at. Fields must never carry secrets,
// tokens, agent keys or full wallet key material: they leave the process.
type AlertEvent struct {
	// Kind is the stable category, e.g. "redeem_wedged". It is a metric label.
	Kind string
	// Key dedupes repeats, e.g. "redeem_wedged:<job id>". Empty falls back to Kind.
	Key      string
	Severity Severity
	Title    string
	Detail   string
	Fields   map[string]string
}

type alerter struct {
	mu       sync.Mutex
	lastSent map[string]time.Time
	cooldown time.Duration
	now      func() time.Time

	webhookURL string
	client     *http.Client
	queue      chan AlertEvent
	done       chan struct{}
	// closed is set under mu before queue is closed, so raise never sends on a closed channel.
	closed bool
}

var (
	alertsMu sync.RWMutex
	alerts   = newAlerter("", DefaultAlertCooldown, nil)
)

func newAlerter(webhookURL string, cooldown time.Duration, client *http.Client) *alerter {
	if cooldown <= 0 {
		cooldown = DefaultAlertCooldown
	}
	if client == nil {
		client = &http.Client{Timeout: alertSendTimeout}
	}
	return &alerter{
		lastSent:   make(map[string]time.Time),
		cooldown:   cooldown,
		now:        time.Now,
		webhookURL: strings.TrimSpace(webhookURL),
		client:     client,
	}
}

// InitAlerts points alert delivery at a Slack- or Discord-compatible incoming webhook.
// An empty URL keeps alerts in the log (and Sentry, when configured) only. The returned
// func drains queued alerts; call it on shutdown.
func InitAlerts(webhookURL string) (func(), error) {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL != "" {
		parsed, err := url.Parse(webhookURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return nil, fmt.Errorf("alert webhook url is not a valid http(s) url")
		}
	}
	next := newAlerter(webhookURL, DefaultAlertCooldown, nil)
	next.start()

	alertsMu.Lock()
	alerts = next
	alertsMu.Unlock()
	return next.stop, nil
}

func (a *alerter) start() {
	if a.webhookURL == "" {
		return
	}
	a.queue = make(chan AlertEvent, alertQueueSize)
	a.done = make(chan struct{})
	go func() {
		defer close(a.done)
		for event := range a.queue {
			a.deliver(event)
		}
	}()
}

func (a *alerter) stop() {
	if a.queue == nil {
		return
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	close(a.queue)
	a.mu.Unlock()
	select {
	case <-a.done:
	case <-time.After(alertDrainTimeout):
	}
}

// Alert raises an alert: always a structured log line, plus Sentry and the webhook when
// configured. Repeats of the same key inside the cooldown are counted but not re-sent.
// It never blocks: a full queue drops the webhook delivery, not the caller.
func Alert(ctx context.Context, event AlertEvent) {
	alertsMu.RLock()
	a := alerts
	alertsMu.RUnlock()
	a.raise(ctx, event)
}

func (a *alerter) raise(ctx context.Context, event AlertEvent) {
	if event.Key == "" {
		event.Key = event.Kind
	}
	if event.Severity == "" {
		event.Severity = SeverityWarning
	}
	if len(event.Detail) > maxAlertDetailSize {
		event.Detail = event.Detail[:maxAlertDetailSize]
	}

	a.mu.Lock()
	now := a.now()
	last, seen := a.lastSent[event.Key]
	suppressed := seen && now.Sub(last) < a.cooldown
	if !suppressed {
		a.lastSent[event.Key] = now
	}
	a.mu.Unlock()

	if suppressed {
		alertsSent.WithLabelValues(event.Kind, "suppressed").Inc()
		return
	}

	attrs := []any{"alert", event.Kind, "alert_key", event.Key, "severity", string(event.Severity), "detail", event.Detail}
	for _, name := range sortedKeys(event.Fields) {
		attrs = append(attrs, name, event.Fields[name])
	}
	if event.Severity == SeverityCritical {
		slog.ErrorContext(ctx, event.Title, attrs...)
	} else {
		slog.WarnContext(ctx, event.Title, attrs...)
	}
	captureAlert(event)

	if a.queue == nil {
		alertsSent.WithLabelValues(event.Kind, "log_only").Inc()
		return
	}
	if !a.enqueue(event) {
		alertsSent.WithLabelValues(event.Kind, "dropped").Inc()
		slog.WarnContext(ctx, "alert webhook delivery dropped: queue full or shutting down", "alert", event.Kind)
	}
}

// enqueue hands the event to the delivery goroutine without blocking. It reports false when
// the queue is full or already closed by shutdown.
func (a *alerter) enqueue(event AlertEvent) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	select {
	case a.queue <- event:
		return true
	default:
		return false
	}
}

func (a *alerter) deliver(event AlertEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), alertSendTimeout)
	defer cancel()

	body, err := json.Marshal(webhookPayload(a.webhookURL, event))
	if err != nil {
		alertsSent.WithLabelValues(event.Kind, "failed").Inc()
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.webhookURL, bytes.NewReader(body))
	if err != nil {
		alertsSent.WithLabelValues(event.Kind, "failed").Inc()
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		alertsSent.WithLabelValues(event.Kind, "failed").Inc()
		// The URL carries the webhook secret, so only the error class is logged.
		slog.Warn("alert webhook delivery failed", "alert", event.Kind, "err_type", fmt.Sprintf("%T", err))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= http.StatusMultipleChoices {
		alertsSent.WithLabelValues(event.Kind, "failed").Inc()
		slog.Warn("alert webhook rejected delivery", "alert", event.Kind, "status", resp.StatusCode)
		return
	}
	alertsSent.WithLabelValues(event.Kind, "sent").Inc()
}

// webhookPayload shapes the message for the receiver: Discord reads "content", Slack and
// Slack-compatible receivers read "text".
func webhookPayload(webhookURL string, event AlertEvent) map[string]string {
	message := alertMessage(event)
	if parsed, err := url.Parse(webhookURL); err == nil && strings.Contains(parsed.Host, "discord") {
		return map[string]string{"content": message}
	}
	return map[string]string{"text": message}
}

func alertMessage(event AlertEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", strings.ToUpper(string(event.Severity)), event.Title)
	if event.Detail != "" {
		fmt.Fprintf(&b, "\n%s", event.Detail)
	}
	for _, name := range sortedKeys(event.Fields) {
		fmt.Fprintf(&b, "\n%s: %s", name, event.Fields[name])
	}
	return b.String()
}

func sortedKeys(fields map[string]string) []string {
	keys := make([]string, 0, len(fields))
	for name := range fields {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}
