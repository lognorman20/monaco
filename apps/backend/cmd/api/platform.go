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

	"github.com/monaco/monaco/apps/backend/internal/httpapi"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/solana/balance"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
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
	// envSentryDSN turns on error reporting for panics and alerts. Unset = off.
	envSentryDSN = "SENTRY_DSN"
	// envAppEnv names the deployment (dev, staging, prod) on Sentry events. Anything other
	// than unset, dev or local also switches stderr logs to JSON.
	envAppEnv = "APP_ENV"
	// envLogFormat overrides the stderr log format: text or json. See logFormat.
	envLogFormat = "LOG_FORMAT"
	// envRelease identifies the build on Sentry events, usually the git sha.
	envRelease = "RELEASE"
	// envAlertWebhookURL is a Slack- or Discord-compatible incoming webhook that receives
	// alerts. Unset = alerts stay in the log (and Sentry).
	envAlertWebhookURL = "ALERT_WEBHOOK_URL"
	// envMetricsToken is the bearer token a Prometheus scraper presents to GET /metrics.
	// Unset = /metrics answers loopback callers only.
	envMetricsToken = "METRICS_TOKEN"

	privyAPIBaseURL = "https://api.privy.io"

	logFormatText = "text"
	logFormatJSON = "json"
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

// logFormat picks the stderr format. On a developer machine (APP_ENV unset, dev or local)
// it is slog text, which is what `just run` tees to .logs/. Anywhere else stderr is what the
// host's log collector stores, so it is JSON unless LOG_FORMAT says otherwise. An unknown
// LOG_FORMAT fails boot: a typo should not silently change what the log pipeline parses.
func logFormat() (string, error) {
	switch format := strings.ToLower(strings.TrimSpace(os.Getenv(envLogFormat))); format {
	case logFormatText, logFormatJSON:
		return format, nil
	case "":
	default:
		return "", fmt.Errorf("%s must be %s or %s, got %q", envLogFormat, logFormatText, logFormatJSON, format)
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envAppEnv))) {
	case "", "dev", "development", "local":
		return logFormatText, nil
	default:
		return logFormatJSON, nil
	}
}

// setupLogging installs the process logger and returns a func that flushes and closes the
// log file. Without LOG_FILE it logs to stderr in the format logFormat picks; with it, JSON
// lines go to both stderr and the file so one format serves the terminal and the durable copy.
func setupLogging() (func(), error) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	format, err := logFormat()
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(os.Getenv(envLogFile))
	if path == "" {
		var handler slog.Handler = slog.NewTextHandler(os.Stderr, opts)
		if format == logFormatJSON {
			handler = slog.NewJSONHandler(os.Stderr, opts)
		}
		slog.SetDefault(slog.New(httpapi.NewRequestIDLogHandler(handler)))
		return func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s %q: %w", envLogFile, path, err)
	}
	handler := slog.NewJSONHandler(io.MultiWriter(os.Stderr, file), opts)
	slog.SetDefault(slog.New(httpapi.NewRequestIDLogHandler(handler)))
	return func() {
		_ = file.Sync()
		_ = file.Close()
	}, nil
}

// trustProxyHeaders reports whether per-address limits may key on X-Forwarded-For.
func trustProxyHeaders() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envTrustProxyHeaders)), "true")
}

// setupTelemetry starts error reporting and alert delivery. The returned func flushes both;
// call it on shutdown and before a fatal exit.
func setupTelemetry() (func(), error) {
	flushSentry, err := telemetry.InitSentry(telemetry.SentryOptions{
		DSN:         os.Getenv(envSentryDSN),
		Environment: os.Getenv(envAppEnv),
		Release:     os.Getenv(envRelease),
	})
	if err != nil {
		return nil, err
	}
	stopAlerts, err := telemetry.InitAlerts(os.Getenv(envAlertWebhookURL))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envAlertWebhookURL, err)
	}
	slog.Info("telemetry ready",
		"sentry", strings.TrimSpace(os.Getenv(envSentryDSN)) != "",
		"alert_webhook", strings.TrimSpace(os.Getenv(envAlertWebhookURL)) != "",
		"metrics_token", strings.TrimSpace(os.Getenv(envMetricsToken)) != "",
	)
	return func() {
		stopAlerts()
		flushSentry()
	}, nil
}

// metricsHandler serves GET /metrics behind the scrape token.
func metricsHandler() http.Handler {
	return httpapi.MetricsEndpoint(os.Getenv(envMetricsToken), telemetry.Handler())
}

// platformHandler wraps the route mux with the cross-cutting middleware. Order matters:
// the request id is set first so every later log line and error body carries it,
// Recover sits outside everything that can panic, and Metrics wraps only Idempotency and the
// mux.
//
// Idempotency is innermost on purpose. Inside Metrics, a replayed response, an in-progress
// 409 and a key-mismatch 422 are counted and timed like any other answer (and carry the
// request id set further out); outside it they would vanish from the dashboards. It hands
// the mux the same *http.Request it received, so Metrics still reads the route pattern the
// mux sets, and for the answers that never reach the mux it fills the pattern in from the
// mux's own route table. Being inside the rate limiter and body cap also means a key is only
// spent on a request that was let through, against a size-capped body.
func platformHandler(mux *http.ServeMux, idempotency *httpapi.Idempotency) http.Handler {
	idempotency.WithRoutePattern(func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	})
	trustProxy := trustProxyHeaders()
	origins := strings.Split(os.Getenv(envCORSAllowedOrigins), ",")
	return httpapi.Chain(mux,
		httpapi.RequestID(),
		httpapi.Recover(),
		httpapi.CORS(origins),
		httpapi.NewRateLimiter(trustProxy).Middleware(),
		httpapi.LimitRequestBody(httpapi.DefaultMaxRequestBytes),
		httpapi.Metrics(),
		idempotency.Middleware(),
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

// registerDatabaseMetrics exports the pool statistics and the money-path backlog.
func registerDatabaseMetrics(db *sql.DB, store *postgres.Store) error {
	if err := telemetry.RegisterDBStats(db, "monaco"); err != nil {
		return fmt.Errorf("register db pool metrics: %w", err)
	}
	err := telemetry.RegisterBacklog(func(ctx context.Context) (telemetry.Backlog, error) {
		backlog, err := store.GetOpsBacklog(ctx)
		return telemetry.Backlog(backlog), err
	})
	if err != nil {
		return fmt.Errorf("register backlog metrics: %w", err)
	}
	return nil
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
		{Name: "relayer_balance", Check: func(ctx context.Context) error {
			return checkRelayerBalance(ctx, solanaRPC, relayerPubkey)
		}},
		{Name: "pollers", Check: telemetry.CheckPollers},
		{Name: "privy", Check: func(ctx context.Context) error {
			return probeReachable(ctx, probeClient, privyAPIBaseURL)
		}},
		{Name: "price_source", Check: func(ctx context.Context) error {
			_, err := prices.Prices(ctx, []string{jupiter.USDCMint})
			return err
		}},
	}
}

// checkRelayerBalance publishes the relayer's SOL balance and fails below the boot minimum.
// Boot refuses to start under that floor, but a running API drains the relayer one fee at a
// time, and once it is empty every sweep, trade and cash out fails. An RPC error is not a
// balance problem: solana_rpc reports it.
func checkRelayerBalance(ctx context.Context, solanaRPC solanaBalanceReader, relayerPubkey string) error {
	lamports, err := solanaRPC.GetBalance(ctx, relayerPubkey)
	if err != nil {
		return nil
	}
	telemetry.SetRelayerBalance(lamports)
	if lamports > balance.FeePayerMinLamports {
		return nil
	}
	telemetry.Alert(ctx, telemetry.AlertEvent{
		Kind:     "relayer_low_balance",
		Severity: telemetry.SeverityCritical,
		Title:    "Relayer SOL balance is below the minimum",
		Detail:   "Top up the relayer: every sweep, trade and cash out pays its fee from this wallet.",
		Fields: map[string]string{
			"relayer_pubkey": relayerPubkey,
			"lamports":       fmt.Sprint(lamports),
			"min_lamports":   fmt.Sprint(balance.FeePayerMinLamports),
		},
	})
	return fmt.Errorf("relayer balance %d lamports is at or below the %d minimum", lamports, balance.FeePayerMinLamports)
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
