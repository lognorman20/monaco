package errreport

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew_noDSN_returnsNoopThatNeverBlocksShutdown(t *testing.T) {
	// Act
	reporter, err := New(Options{Environment: "local"})

	// Assert
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := reporter.(Noop); !ok {
		t.Fatalf("reporter = %T, want Noop when SENTRY_DSN is unset", reporter)
	}
	reporter.Report(context.Background(), Event{Message: "dropped"})
	if !reporter.Flush(time.Millisecond) {
		t.Fatal("Noop.Flush = false")
	}
}

func TestNew_malformedDSN_failsWithoutEchoingIt(t *testing.T) {
	// Act
	_, err := New(Options{DSN: "https://publickey:secretpart@not a host/1"})

	// Assert
	if err == nil {
		t.Fatal("New accepted a malformed DSN")
	}
	if strings.Contains(err.Error(), "secretpart") {
		t.Fatalf("error leaks the DSN: %v", err)
	}
}

// sentryIngest is a stand-in Sentry endpoint that records what would leave the process.
type sentryIngest struct {
	mu     sync.Mutex
	bodies []string
}

func (s *sentryIngest) handler(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, string(raw))
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"id":"0"}`))
}

func (s *sentryIngest) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.bodies, "\n")
}

func TestSentryReporter_flushDeliversScrubbedEventWithEnvironmentAndRelease(t *testing.T) {
	// Arrange
	ingest := &sentryIngest{}
	server := httptest.NewServer(http.HandlerFunc(ingest.handler))
	defer server.Close()
	dsn := strings.Replace(server.URL, "http://", "http://publickey@", 1) + "/42"
	reporter, err := New(Options{DSN: dsn, Environment: "staging", Release: "abc123"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Act
	reporter.Report(context.Background(), Event{
		Level:   LevelFatal,
		Message: "worker panic",
		Panic:   "privy rejected Authorization: Bearer live-access-token",
		Stack:   "goroutine 7 [running]",
		Tags:    map[string]string{"worker": "sweep_poller"},
		Extra:   map[string]any{"X-Monaco-Agent-Key": "live-agent-key"},
	})
	flushed := reporter.Flush(5 * time.Second)

	// Assert
	if !flushed {
		t.Fatal("Flush = false: the crash report would be lost on shutdown")
	}
	sent := ingest.all()
	for _, want := range []string{`"environment":"staging"`, `"release":"abc123"`, `"worker":"sweep_poller"`, `"level":"fatal"`} {
		if !strings.Contains(sent, want) {
			t.Errorf("event missing %s in:\n%s", want, sent)
		}
	}
	for _, secret := range []string{"live-access-token", "live-agent-key"} {
		if strings.Contains(sent, secret) {
			t.Errorf("secret %q reached the wire:\n%s", secret, sent)
		}
	}
}

func TestSentryReporter_ingestDown_reportDoesNotBlockOrPanic(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.NotFoundHandler())
	dsn := strings.Replace(server.URL, "http://", "http://publickey@", 1) + "/42"
	server.Close()
	reporter, err := New(Options{DSN: dsn})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)
		reporter.Report(context.Background(), Event{Message: "sweep failed", Err: errors.New("rpc down")})
	}()

	// Assert
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Report blocked the money path on an unreachable Sentry")
	}
	reporter.Flush(time.Second)
}

type captureReporter struct {
	events []Event
}

func (c *captureReporter) Report(_ context.Context, event Event) { c.events = append(c.events, event) }
func (c *captureReporter) Flush(time.Duration) bool              { return true }

func TestSlogHandler_reportsErrorRecordsOnly_withLoggerAttrs(t *testing.T) {
	// Arrange
	reporter := &captureReporter{}
	logger := slog.New(NewSlogHandler(slog.NewTextHandler(io.Discard, nil), reporter)).With("worker", "sweep_poller")

	// Act
	logger.Warn("sweep slow", "deposit_id", "d0")
	logger.Error("sweep poller tick failed", "deposit_id", "d1", "err", errors.New("rpc down"))

	// Assert
	if len(reporter.events) != 1 {
		t.Fatalf("reported %d events, want 1", len(reporter.events))
	}
	event := reporter.events[0]
	if event.Level != LevelError || event.Message != "sweep poller tick failed" {
		t.Fatalf("event = %+v", event)
	}
	if event.Err == nil || event.Err.Error() != "rpc down" {
		t.Fatalf("err = %v", event.Err)
	}
	if event.Tags["worker"] != "sweep_poller" || event.Extra["deposit_id"] != "d1" {
		t.Fatalf("tags = %v extra = %v", event.Tags, event.Extra)
	}
}

func TestSlogHandler_panicRecord_isLeftToTheRecoverer(t *testing.T) {
	// Arrange
	reporter := &captureReporter{}
	logger := slog.New(NewSlogHandler(slog.NewTextHandler(io.Discard, nil), reporter))

	// Act
	logger.Error("worker panic", PanicAttrKey, "boom", "stack", "...")

	// Assert
	if len(reporter.events) != 0 {
		t.Fatalf("panic log line reported %d events; the recoverer already reports it with the live stack", len(reporter.events))
	}
}

func TestNewSlogHandler_noopReporter_returnsInnerHandler(t *testing.T) {
	// Arrange
	inner := slog.NewTextHandler(io.Discard, nil)

	// Act / Assert
	if got := NewSlogHandler(inner, Noop{}); got != slog.Handler(inner) {
		t.Fatalf("handler = %T, want the inner handler untouched when reporting is off", got)
	}
}
