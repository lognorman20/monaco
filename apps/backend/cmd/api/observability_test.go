package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/errreport"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/metrics"
)

type recordingReporter struct {
	mu     sync.Mutex
	events []errreport.Event
}

func (r *recordingReporter) Report(_ context.Context, event errreport.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingReporter) Flush(time.Duration) bool { return true }

func (r *recordingReporter) snapshot() []errreport.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]errreport.Event(nil), r.events...)
}

func TestAPIServer_missingPrivyVerificationKey_failsStartup(t *testing.T) {
	// Arrange
	clearAPIEnv(t)
	setValidAPIEnv(t)
	t.Setenv("PRIVY_VERIFICATION_KEY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	_, err := boot(ctx, nil)

	// Assert
	if err == nil {
		t.Fatal("boot succeeded without PRIVY_VERIFICATION_KEY; every authenticated route would 500")
	}
	if !strings.Contains(err.Error(), "PRIVY_VERIFICATION_KEY") {
		t.Fatalf("error = %q, want PRIVY_VERIFICATION_KEY mentioned", err.Error())
	}
}

func TestAPIServer_malformedPrivyVerificationKey_failsStartup(t *testing.T) {
	// Arrange
	clearAPIEnv(t)
	setValidAPIEnv(t)
	t.Setenv("PRIVY_VERIFICATION_KEY", "not-a-pem")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	_, err := boot(ctx, nil)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "PRIVY_VERIFICATION_KEY") {
		t.Fatalf("err = %v, want a PRIVY_VERIFICATION_KEY parse error", err)
	}
}

// captureStderr swaps os.Stderr for a pipe until the returned func is called.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writer
	return func() string {
		os.Stderr = original
		_ = writer.Close()
		raw, _ := io.ReadAll(reader)
		return string(raw)
	}
}

func TestSetupLogging_outsideLocal_writesJSONToStderr(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, "")
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOG_FORMAT", "")
	observability, err := config.LoadObservability()
	if err != nil {
		t.Fatalf("LoadObservability: %v", err)
	}
	restore := captureStderr(t)

	// Act
	_, err = setupLogging(observability, nil)
	slog.Info("sweep confirmed", "deposit_id", "d1")
	output := restore()

	// Assert
	if err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace([]byte(output)), &line); err != nil {
		t.Fatalf("stderr line is not JSON: %v (%q)", err, output)
	}
	if line["msg"] != "sweep confirmed" || line["deposit_id"] != "d1" {
		t.Fatalf("log line = %v", line)
	}
}

func TestSetupLogging_localDefault_keepsTextOnStderr(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, "")
	t.Setenv("APP_ENV", "")
	t.Setenv("LOG_FORMAT", "")
	observability, err := config.LoadObservability()
	if err != nil {
		t.Fatalf("LoadObservability: %v", err)
	}
	restore := captureStderr(t)

	// Act
	_, err = setupLogging(observability, nil)
	slog.Info("sweep confirmed", "deposit_id", "d1")
	output := restore()

	// Assert
	if err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	if !strings.Contains(output, "msg=\"sweep confirmed\"") || !strings.Contains(output, "deposit_id=d1") {
		t.Fatalf("stderr = %q, want slog text output for the just run tee", output)
	}
}

func TestSetupLogging_errorRecord_isReportedWithRequestID(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, "")
	reporter := &recordingReporter{}
	restore := captureStderr(t)
	defer restore()
	if _, err := setupLogging(config.Observability{LogFormat: config.LogFormatJSON}, reporter); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	ctx := httpapi.WithRequestID(context.Background(), "req-9")

	// Act
	slog.InfoContext(ctx, "cash out started")
	slog.ErrorContext(ctx, "redeem recovery failed", "job_id", "j1", "err", errors.New("rpc timeout"))

	// Assert
	events := reporter.snapshot()
	if len(events) != 1 {
		t.Fatalf("reported %d events, want only the error record", len(events))
	}
	event := events[0]
	if event.Message != "redeem recovery failed" || event.Err == nil || event.Err.Error() != "rpc timeout" {
		t.Fatalf("event = %+v", event)
	}
	if event.Extra["request_id"] != "req-9" || event.Extra["job_id"] != "j1" {
		t.Fatalf("extra = %v, want request_id and job_id", event.Extra)
	}
}

func TestPlatformHandler_handlerPanic_isReportedOnceAndCountedAs500(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, "")
	reporter := &recordingReporter{}
	restore := captureStderr(t)
	defer restore()
	if _, err := setupLogging(config.Observability{LogFormat: config.LogFormatJSON}, reporter); err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	opsMetrics := metrics.New()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/groups/{id}/fund", func(http.ResponseWriter, *http.Request) {
		panic("nil treasury")
	})
	handler := platformHandler(mux, reporter, opsMetrics)
	req := httptest.NewRequest(http.MethodPost, "/v1/groups/g1/fund", nil)
	req.Header.Set("Authorization", "Bearer secret-access-token")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	events := reporter.snapshot()
	if len(events) != 1 {
		t.Fatalf("reported %d events, want exactly one: the panic log line must not be reported again", len(events))
	}
	event := events[0]
	if event.Level != errreport.LevelFatal || event.Panic != "nil treasury" || event.Stack == "" {
		t.Fatalf("event = %+v, want a fatal panic with a stack", event)
	}
	if event.Tags["route"] != "POST /v1/groups/{id}/fund" {
		t.Fatalf("route tag = %q", event.Tags["route"])
	}

	scrape := httptest.NewRecorder()
	opsMetrics.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	want := `monaco_http_requests_total{method="POST",route="POST /v1/groups/{id}/fund",status="500"} 1`
	if !strings.Contains(scrape.Body.String(), want) {
		t.Fatalf("metrics missing %q in:\n%s", want, scrape.Body.String())
	}
}

type fakeVerifier struct{ err error }

func (f fakeVerifier) VerifierReady() error { return f.err }

type fakeBalanceReader struct{}

func (fakeBalanceReader) GetBalance(context.Context, string) (uint64, error) { return 1, nil }

func TestHealthChecks_authVerifierNotReady_isCriticalFailure(t *testing.T) {
	// Arrange
	checks := healthChecks(nil, fakeBalanceReader{}, "relayer", nil, fakeVerifier{err: errors.New("no key")})

	// Act
	var found *httpapi.HealthCheck
	for i := range checks {
		if checks[i].Name == "auth_verifier" {
			found = &checks[i]
		}
	}

	// Assert
	if found == nil {
		t.Fatal("auth_verifier health check missing")
	}
	if !found.Critical {
		t.Fatal("auth_verifier must be critical: without it no authenticated route works")
	}
	if err := found.Check(context.Background()); err == nil {
		t.Fatal("check passed with an unready verifier")
	}
}
