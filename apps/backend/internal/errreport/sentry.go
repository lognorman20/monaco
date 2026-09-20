package errreport

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryReporter is the only file that knows about Sentry.
type sentryReporter struct {
	hub *sentry.Hub
}

func newSentryReporter(opts Options) (*sentryReporter, error) {
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:         opts.DSN,
		Environment: opts.Environment,
		Release:     opts.Release,
		// No IPs, cookies or request bodies: the API handles wallet credentials.
		SendDefaultPII: false,
		BeforeSend:     beforeSend,
		// ContextifyFrames reads source files from disk and ships the lines around each
		// frame. Function, file and line are enough; file contents stay on the host.
		Integrations: func(integrations []sentry.Integration) []sentry.Integration {
			kept := integrations[:0]
			for _, integration := range integrations {
				if integration.Name() != "ContextifyFrames" {
					kept = append(kept, integration)
				}
			}
			return kept
		},
	})
	if err != nil {
		// The DSN embeds the project key, so the error names the variable, not the value.
		return nil, fmt.Errorf("SENTRY_DSN rejected by the Sentry client: %s", ScrubString(err.Error()))
	}
	return &sentryReporter{hub: sentry.NewHub(client, sentry.NewScope())}, nil
}

func (r *sentryReporter) Report(_ context.Context, event Event) {
	r.hub.CaptureEvent(toSentryEvent(Scrub(event)))
}

func (r *sentryReporter) Flush(timeout time.Duration) bool {
	return r.hub.Flush(timeout)
}

// toSentryEvent maps an already scrubbed Event.
func toSentryEvent(event Event) *sentry.Event {
	out := sentry.NewEvent()
	out.Level = sentry.LevelError
	if event.Level == LevelFatal {
		out.Level = sentry.LevelFatal
	}
	out.Message = event.Message
	for k, v := range event.Tags {
		out.Tags[k] = v
	}
	if len(event.Extra) > 0 {
		out.Contexts["log"] = event.Extra
	}

	switch {
	case event.Panic != nil:
		// Called from the deferred recover, so the live stack still holds the panicking
		// frames; Sentry groups panics by them.
		out.Exception = []sentry.Exception{{
			Type:       "panic",
			Value:      fmt.Sprint(event.Panic),
			Stacktrace: sentry.NewStacktrace(),
		}}
		if event.Stack != "" {
			out.Contexts["panic"] = map[string]any{"stack": event.Stack}
		}
	case event.Err != nil:
		out.Exception = []sentry.Exception{{Type: event.Message, Value: event.Err.Error()}}
		// Error text carries ids and amounts; group by the log message instead.
		out.Fingerprint = []string{event.Message}
	default:
		out.Fingerprint = []string{event.Message}
	}
	return out
}

// beforeSend is the last gate before the network. Events built here are scrubbed already;
// this covers whatever the SDK attaches on its own.
func beforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	event.Request = nil
	event.User = sentry.User{}
	event.Message = ScrubString(event.Message)
	for i := range event.Exception {
		event.Exception[i].Value = ScrubString(event.Exception[i].Value)
	}
	for _, crumb := range event.Breadcrumbs {
		crumb.Message = ScrubString(crumb.Message)
		crumb.Data = ScrubMap(crumb.Data)
	}
	return event
}
