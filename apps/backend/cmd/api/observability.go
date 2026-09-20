package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	"github.com/monaco/monaco/apps/backend/internal/errreport"
	"github.com/monaco/monaco/apps/backend/internal/metrics"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/worker"
)

// errorReportFlushTimeout is how long shutdown waits for queued crash reports. A process
// that dies on a boot error or a fatal server error must still get its report out.
const errorReportFlushTimeout = 5 * time.Second

// newErrorReporter returns the Sentry reporter when SENTRY_DSN is set, else a no-op.
func newErrorReporter(observability config.Observability) (errreport.Reporter, error) {
	return errreport.New(errreport.Options{
		DSN:         observability.SentryDSN,
		Environment: observability.AppEnv,
		Release:     releaseTag(observability),
	})
}

// releaseTag is APP_RELEASE, else the git revision `go build` stamps into the binary
// (absent under `go run`), else empty.
func releaseTag(observability config.Observability) string {
	if observability.Release != "" {
		return observability.Release
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

// newOpsMetrics builds the metrics registry: worker ticks and restarts, HTTP, the database
// pool, and the money-path backlog read from Postgres and the fee payer balance.
func newOpsMetrics(db *sql.DB, store *postgres.Store, solanaRPC solanaBalanceReader, relayerPubkey string) *metrics.Metrics {
	m := metrics.New()
	m.RegisterWorkers(worker.NameSweep, worker.NameProposalExecute, worker.NameRedeemRecovery)
	m.RegisterDB(db)
	m.RegisterOps(metrics.OpsSource{
		Backlog: func(ctx context.Context) (metrics.OpsBacklog, error) {
			backlog, err := store.GetOpsBacklog(ctx)
			if err != nil {
				return metrics.OpsBacklog{}, err
			}
			return metrics.OpsBacklog(backlog), nil
		},
		FeePayerLamports: func(ctx context.Context) (uint64, error) {
			return solanaRPC.GetBalance(ctx, relayerPubkey)
		},
	})
	return m
}

// newMetricsServer returns the /metrics listener, or nil when METRICS_ADDR=off.
func newMetricsServer(observability config.Observability, m *metrics.Metrics) *http.Server {
	if !observability.MetricsEnabled() {
		slog.Warn("metrics listener disabled", "reason", "METRICS_ADDR=off")
		return nil
	}
	return m.NewServer(observability.MetricsAddr, observability.MetricsToken)
}

// serveMetrics starts the metrics listener. Losing it must not take the money-moving API
// down with it, so a listen failure is an error log (and a crash report), not an exit.
func serveMetrics(server *http.Server) {
	if server == nil {
		return
	}
	slog.Info("metrics listening", "addr", server.Addr, "path", "/metrics")
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server failed; the API keeps serving without /metrics", "addr", server.Addr, "err", err)
		}
	}()
}

func shutdownMetrics(ctx context.Context, server *http.Server) {
	if server == nil {
		return
	}
	if err := server.Shutdown(ctx); err != nil {
		slog.Warn("metrics shutdown failed", "err", err)
	}
}
