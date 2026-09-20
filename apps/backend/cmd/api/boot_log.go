package main

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/config"
)

func logConfigLoaded(cfg *config.Config) {
	attrs := []any{
		"dynamic_environment_id", cfg.DynamicEnvironmentID,
		"base_rpc_url", cfg.BaseRPCURL,
		"signer_url", cfg.SignerURL,
		"pyth_configured", cfg.PythAPIKey != "",
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
