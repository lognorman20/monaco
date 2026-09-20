package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthHandler_returns200AndOkBody(t *testing.T) {
	// Arrange
	handler := newTestHealthHandler()
	req := testHTTPRequest("GET", "/health")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", payload["status"])
	}
}

func TestHealthHandler_setsContentTypeJson(t *testing.T) {
	// Arrange
	handler := newTestHealthHandler()
	req := testHTTPRequest("GET", "/health")
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, req)

	// Assert
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

type healthPayload struct {
	Status string `json:"status"`
	Checks map[string]struct {
		Status string `json:"status"`
	} `json:"checks"`
}

func serveHealth(t *testing.T, handler *HealthHandlers) (int, healthPayload, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.HealthHandler(rec, testHTTPRequest("GET", "/health"))
	var payload healthPayload
	raw := rec.Body.String()
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return rec.Code, payload, raw
}

func TestHealthHandler_criticalDependencyDown_returns503(t *testing.T) {
	// Arrange
	handler := &HealthHandlers{Checks: []HealthCheck{
		{Name: "database", Critical: true, Check: func(context.Context) error { return errors.New("connection refused") }},
		{Name: "privy", Check: func(context.Context) error { return nil }},
	}}

	// Act
	code, payload, _ := serveHealth(t, handler)

	// Assert
	if code != 503 {
		t.Fatalf("status = %d, want 503", code)
	}
	if payload.Status != "down" {
		t.Fatalf("status field = %q, want down", payload.Status)
	}
	if payload.Checks["database"].Status != "down" || payload.Checks["privy"].Status != "ok" {
		t.Fatalf("checks = %+v", payload.Checks)
	}
}

func TestHealthHandler_nonCriticalDependencyDown_returns200Degraded(t *testing.T) {
	// Arrange
	handler := &HealthHandlers{Checks: []HealthCheck{
		{Name: "database", Critical: true, Check: func(context.Context) error { return nil }},
		{Name: "solana_rpc", Check: func(context.Context) error {
			return errors.New("Post https://rpc.example/?api-key=SECRET: 502")
		}},
	}}

	// Act
	code, payload, raw := serveHealth(t, handler)

	// Assert
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if payload.Status != "degraded" {
		t.Fatalf("status field = %q, want degraded", payload.Status)
	}
	if strings.Contains(raw, "SECRET") {
		t.Fatalf("probe error leaked into the public body: %s", raw)
	}
}

func TestHealthHandler_probeIgnoringContext_timesOutAsDown(t *testing.T) {
	// Arrange
	release := make(chan struct{})
	defer close(release)
	handler := &HealthHandlers{
		Timeout: 20 * time.Millisecond,
		Checks: []HealthCheck{{Name: "privy", Check: func(context.Context) error {
			<-release
			return nil
		}}},
	}

	// Act
	code, payload, _ := serveHealth(t, handler)

	// Assert
	if code != 200 || payload.Checks["privy"].Status != "down" {
		t.Fatalf("code = %d checks = %+v, want 200 with privy down", code, payload.Checks)
	}
}

func TestHealthHandler_panickingProbe_isReportedDown(t *testing.T) {
	// Arrange
	handler := &HealthHandlers{Checks: []HealthCheck{
		{Name: "database", Critical: true, Check: func(context.Context) error { panic("nil pool") }},
	}}

	// Act
	code, _, _ := serveHealth(t, handler)

	// Assert
	if code != 503 {
		t.Fatalf("status = %d, want 503", code)
	}
}

func TestHealthHandler_reusesProbeRoundWithinCacheTTL(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	handler := &HealthHandlers{
		CacheTTL: 10 * time.Second,
		Now:      func() time.Time { return now },
		Checks: []HealthCheck{{Name: "database", Critical: true, Check: func(context.Context) error {
			calls.Add(1)
			return nil
		}}},
	}

	// Act
	serveHealth(t, handler)
	serveHealth(t, handler)
	now = now.Add(11 * time.Second)
	serveHealth(t, handler)

	// Assert
	if got := calls.Load(); got != 2 {
		t.Fatalf("probe calls = %d, want 2 (second request served from cache)", got)
	}
}
