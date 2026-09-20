package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/errreport"
	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/metrics"
)

const (
	// envLogFile is an optional path that receives every log line as JSON, appended.
	// stderr dies with the terminal; the file is what is left to read after an incident.
	envLogFile = "LOG_FILE"
	// envCORSAllowedOrigins is a comma-separated list of browser origins allowed to call
	// the API. Empty (the default) allows none: the only first-party client is the iOS app.
	envCORSAllowedOrigins = "CORS_ALLOWED_ORIGINS"
	// envTrustProxyHeaders makes rate limiting key on X-Forwarded-For. Only set it behind a
	// proxy that overwrites the header.
	envTrustProxyHeaders = "TRUST_PROXY_HEADERS"

	privyAPIBaseURL = "https://api.privy.io"
)

// Server timeouts. WriteTimeout is long because cash out and trade routes sign and confirm
// Solana transactions inside the request; it exists to bound a stuck handler, not to race
// a healthy one. ReadHeaderTimeout is what stops slow-header connection hoarding.
const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 3 * time.Minute
	serverIdleTimeout       = 2 * time.Minute
	serverMaxHeaderBytes    = 64 << 10

	// shutdownGracePeriod is how long in-flight requests get to finish after SIGTERM.
	shutdownGracePeriod = 30 * time.Second
	// workerStopTimeout is how long shutdown waits for a poller tick to unwind.
	workerStopTimeout = 10 * time.Second
)

// setupLogging installs the process logger and returns a func that flushes and closes the
// log file. stderr gets JSON lines everywhere except APP_ENV=local (LOG_FORMAT overrides),
// because outside a terminal stderr is what the host's log collector stores and ships. With
// LOG_FILE, JSON lines go to both stderr and the file so one format serves both copies.
// Error records are also handed to reporter; the request id is added first so reports carry it.
func setupLogging(observability config.Observability, reporter errreport.Reporter) (func(), error) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	install := func(handler slog.Handler) {
		slog.SetDefault(slog.New(httpapi.NewRequestIDLogHandler(errreport.NewSlogHandler(handler, reporter))))
	}
	path := strings.TrimSpace(os.Getenv(envLogFile))
	if path == "" {
		if observability.LogFormat == config.LogFormatJSON {
			install(slog.NewJSONHandler(os.Stderr, opts))
		} else {
			install(slog.NewTextHandler(os.Stderr, opts))
		}
		return func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s %q: %w", envLogFile, path, err)
	}
	install(slog.NewJSONHandler(io.MultiWriter(os.Stderr, file), opts))
	return func() {
		_ = file.Sync()
		_ = file.Close()
	}, nil
}

// platformHandler wraps the route mux with the cross-cutting middleware. Order matters:
// the request id is set first so every later log line and error body carries it, and
// Recover sits outside everything that can panic. The metrics middleware wraps Recover so a
// panic is counted as the 500 it becomes; nothing inside it may copy the request, or the
// route pattern the mux records is lost to it.
func platformHandler(mux http.Handler, reporter errreport.Reporter, opsMetrics *metrics.Metrics) http.Handler {
	trustProxy := strings.EqualFold(strings.TrimSpace(os.Getenv(envTrustProxyHeaders)), "true")
	origins := strings.Split(os.Getenv(envCORSAllowedOrigins), ",")
	return httpapi.Chain(mux,
		httpapi.RequestID(),
		opsMetrics.HTTPMiddleware(),
		httpapi.Recover(reporter),
		httpapi.CORS(origins),
		httpapi.NewRateLimiter(trustProxy).Middleware(),
		httpapi.LimitRequestBody(httpapi.DefaultMaxRequestBytes),
	)
}

// newHTTPServer returns the API server with timeouts set; the zero-value http.Server has none.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
}

// waitWorkers blocks until every poller goroutine returns, or timeout. It reports whether
// they all returned.
func waitWorkers(workers *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// solanaBalanceReader is the slice of the Solana RPC client the health probe needs.
type solanaBalanceReader interface {
	GetBalance(ctx context.Context, address string) (uint64, error)
}

// authVerifier is the slice of the Privy client the health probe needs.
type authVerifier interface {
	VerifierReady() error
}

// healthChecks lists the dependencies GET /health reports. Postgres and the access-token
// verifier are critical: without either no authenticated route can answer. An outage at
// Solana RPC, Privy or the price API degrades money routes but still leaves reads working.
func healthChecks(db *sql.DB, solanaRPC solanaBalanceReader, relayerPubkey string, prices jupiter.PriceClient, verifier authVerifier) []httpapi.HealthCheck {
	probeClient := &http.Client{Timeout: httpapi.DefaultHealthCheckTimeout}
	return []httpapi.HealthCheck{
		{Name: "database", Critical: true, Check: db.PingContext},
		{Name: "auth_verifier", Critical: true, Check: func(context.Context) error {
			return verifier.VerifierReady()
		}},
		{Name: "solana_rpc", Check: func(ctx context.Context) error {
			_, err := solanaRPC.GetBalance(ctx, relayerPubkey)
			return err
		}},
		{Name: "privy", Check: func(ctx context.Context) error {
			return probeReachable(ctx, probeClient, privyAPIBaseURL)
		}},
		{Name: "price_source", Check: func(ctx context.Context) error {
			_, err := prices.Prices(ctx, []string{jupiter.USDCMint})
			return err
		}},
	}
}

// probeReachable reports whether url answers at all. Any status below 500 counts: the
// probe is unauthenticated, so a 401/404 still proves DNS, TLS and the service are up.
func probeReachable(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
