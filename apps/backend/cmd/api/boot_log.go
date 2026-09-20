package main

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

func logConfigLoaded(cfg *config.Config) {
	attrs := []any{
		"solana_cluster", cfg.SolanaCluster,
		"privy_app_id", cfg.PrivyAppID,
		"privy_authorization_configured", cfg.PrivyAuthorizationPrivateKey != "",
		"pyth_configured", cfg.PythAPIKey != "",
		"app_env", cfg.Observability.AppEnv,
		"error_reporting", cfg.Observability.SentryDSN != "",
		// The RPC URL embeds its API key, so only whether one is configured is logged.
		"solana_rpc_configured", cfg.SolanaRPCURL != "",
		"db_max_open_conns", cfg.DBPool.MaxOpenConns,
	}
	attrs = append(attrs, databaseLogAttrs(cfg.DatabaseURL)...)
	slog.Info("config loaded", attrs...)
}

func databaseLogAttrs(databaseURL string) []any {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return []any{"database_host", "unknown", "database_name", "unknown"}
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		name = "unknown"
	}
	host := parsed.Host
	if host == "" {
		host = "unknown"
	}
	return []any{"database_host", host, "database_name", name}
}

func logRoutesReady(routes []string) {
	slog.Info("routes ready", "count", len(routes))
	for _, route := range routes {
		slog.Debug("route registered", "route", route)
	}
}
