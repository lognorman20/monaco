package telemetry

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

const sentryFlushTimeout = 3 * time.Second

// sentryEnabled gates every capture so an unconfigured API never touches the SDK.
var sentryEnabled atomic.Bool

// SentryOptions configures error reporting. An empty DSN disables it.
type SentryOptions struct {
	DSN         string
	Environment string
	Release     string
}

// InitSentry turns on error reporting when a DSN is set. The returned func flushes
// buffered events; call it on shutdown and before a fatal exit.
func InitSentry(opts SentryOptions) (func(), error) {
	dsn := strings.TrimSpace(opts.DSN)
	if dsn == "" {
		return func() {}, nil
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: strings.TrimSpace(opts.Environment),
		Release:     strings.TrimSpace(opts.Release),
		// Request bodies and headers carry bearer tokens and agent keys. Events are built
		// by hand below from values that are safe to leave the process.
		SendDefaultPII:   false,
		AttachStacktrace: true,
		BeforeSend:       scrubSentryEvent,
	})
	if err != nil {
		return nil, fmt.Errorf("init sentry: %w", err)
	}
	sentryEnabled.Store(true)
	return func() { sentry.Flush(sentryFlushTimeout) }, nil
}

// scrubSentryEvent is the last gate before the network. A panic value or an alert detail is
// often an upstream error, and those quote what was sent: an RPC URL with its api-key, a
// bearer token, a database URL. SendDefaultPII does not cover any of that.
func scrubSentryEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}
	event.Message = ScrubString(event.Message)
	for i := range event.Exception {
		event.Exception[i].Value = ScrubString(event.Exception[i].Value)
	}
	event.Tags = ScrubTags(event.Tags)
	for name, fields := range event.Contexts {
		event.Contexts[name] = ScrubMap(fields)
	}
	for _, crumb := range event.Breadcrumbs {
		crumb.Message = ScrubString(crumb.Message)
		crumb.Data = ScrubMap(crumb.Data)
	}
	return event
}

// CapturePanic reports a recovered panic with the stack captured at the recover site.
func CapturePanic(recovered any, stack []byte, tags map[string]string) {
	if !sentryEnabled.Load() {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelFatal)
		scope.SetTags(tags)
		scope.SetContext("panic", sentry.Context{"stack": string(stack)})
		sentry.CurrentHub().Recover(recovered)
	})
}

func captureAlert(event AlertEvent) {
	if !sentryEnabled.Load() {
		return
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		level := sentry.LevelWarning
		if event.Severity == SeverityCritical {
			level = sentry.LevelError
		}
		scope.SetLevel(level)
		scope.SetTag("alert", event.Kind)
		scope.SetFingerprint([]string{"alert", event.Kind})
		details := sentry.Context{"detail": event.Detail}
		for name, value := range event.Fields {
			details[name] = value
		}
		scope.SetContext("alert", details)
		sentry.CaptureMessage(event.Title)
	})
}
