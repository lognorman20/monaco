package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAPIServer_missingPrivyVerificationKey_failsStartup(t *testing.T) {
	// Arrange
	clearAPIEnv(t)
	setValidAPIEnv(t)
	t.Setenv("PRIVY_VERIFICATION_KEY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	_, err := boot(ctx)

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
	_, err := boot(ctx)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "PRIVY_VERIFICATION_KEY") {
		t.Fatalf("err = %v, want a PRIVY_VERIFICATION_KEY parse error", err)
	}
}

type fakeVerifier struct{ err error }

func (f fakeVerifier) VerifierReady() error { return f.err }

func TestHealthChecks_authVerifierNotReady_isCriticalFailure(t *testing.T) {
	// Arrange
	checks := healthChecks(nil, stubBalanceReader{}, "pubkey", nil, fakeVerifier{err: errors.New("no key")})

	// Act / Assert
	for _, check := range checks {
		if check.Name != "auth_verifier" {
			continue
		}
		if !check.Critical {
			t.Fatal("auth_verifier must be critical: without it no authenticated route works")
		}
		if err := check.Check(context.Background()); err == nil {
			t.Fatal("check passed with an unready verifier")
		}
		return
	}
	t.Fatal("auth_verifier health check missing")
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

func TestSetupLogging_stderrFormat(t *testing.T) {
	cases := map[string]struct {
		appEnv, logFormat string
		wantJSON          bool
	}{
		"unset env keeps text for the just run tee": {},
		"dev keeps text":                  {appEnv: "dev"},
		"prod defaults to json":           {appEnv: "prod", wantJSON: true},
		"staging defaults to json":        {appEnv: "Staging", wantJSON: true},
		"LOG_FORMAT=text overrides prod":  {appEnv: "prod", logFormat: "text"},
		"LOG_FORMAT=json overrides local": {logFormat: "json", wantJSON: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			previous := slog.Default()
			t.Cleanup(func() { slog.SetDefault(previous) })
			t.Setenv(envLogFile, "")
			t.Setenv(envAppEnv, tc.appEnv)
			t.Setenv(envLogFormat, tc.logFormat)
			restore := captureStderr(t)

			// Act
			_, err := setupLogging()
			slog.Info("sweep confirmed", "deposit_id", "d1")
			output := restore()

			// Assert
			if err != nil {
				t.Fatalf("setupLogging: %v", err)
			}
			var line map[string]any
			isJSON := json.Unmarshal(bytes.TrimSpace([]byte(output)), &line) == nil
			if isJSON != tc.wantJSON {
				t.Fatalf("json = %v, want %v; stderr = %q", isJSON, tc.wantJSON, output)
			}
			if !strings.Contains(output, "sweep confirmed") || !strings.Contains(output, "d1") {
				t.Fatalf("stderr = %q, want the log line", output)
			}
		})
	}
}

func TestSetupLogging_unknownLogFormat_failsBoot(t *testing.T) {
	// Arrange
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envLogFile, "")
	t.Setenv(envLogFormat, "yaml")

	// Act
	_, err := setupLogging()

	// Assert
	if err == nil || !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Fatalf("err = %v, want LOG_FORMAT rejected", err)
	}
}
