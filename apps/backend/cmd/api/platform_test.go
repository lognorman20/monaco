package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
)

func TestSetupLogging_logFileSet_appendsJSONLinesWithRequestID(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	path := filepath.Join(t.TempDir(), "api.log")
	t.Setenv(envLogFile, path)

	// Act
	closeLog, err := setupLogging(config.Observability{LogFormat: config.LogFormatText}, nil)
	if err != nil {
		t.Fatalf("setupLogging: %v", err)
	}
	slog.InfoContext(httpapi.WithRequestID(context.Background(), "req-1"), "cash out started", "group_id", "g1")
	closeLog()

	// Assert
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &line); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, raw)
	}
	if line["msg"] != "cash out started" || line["request_id"] != "req-1" || line["group_id"] != "g1" {
		t.Fatalf("log line = %v", line)
	}
}

func TestSetupLogging_unwritableLogFile_failsBoot(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, filepath.Join(t.TempDir(), "missing-dir", "api.log"))

	// Act
	_, err := setupLogging(config.Observability{LogFormat: config.LogFormatText}, nil)

	// Assert
	if err == nil {
		t.Fatal("setupLogging succeeded, want an error so logs are never silently dropped")
	}
}

func TestProbeReachable(t *testing.T) {
	cases := map[string]struct {
		status  int
		wantErr bool
	}{
		"unauthenticated 401 still proves the service is up": {status: http.StatusUnauthorized},
		"5xx is down": {status: http.StatusBadGateway, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			// Act
			err := probeReachable(context.Background(), server.Client(), server.URL)

			// Assert
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestProbeReachable_connectionRefused_isDown(t *testing.T) {
	// Arrange
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	// Act
	err := probeReachable(context.Background(), &http.Client{Timeout: time.Second}, url)

	// Assert
	if err == nil {
		t.Fatal("err = nil, want a connection error")
	}
}

func TestWaitWorkers_reportsTimeout(t *testing.T) {
	// Arrange
	workers := &sync.WaitGroup{}
	workers.Add(1)
	defer workers.Done()

	// Act / Assert
	if waitWorkers(workers, 10*time.Millisecond) {
		t.Fatal("waitWorkers = true while a worker is still running")
	}
}
