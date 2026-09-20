package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	envAppEnv       = "APP_ENV"
	envAppRelease   = "APP_RELEASE"
	envSentryDSN    = "SENTRY_DSN"
	envLogFormat    = "LOG_FORMAT"
	envMetricsAddr  = "METRICS_ADDR"
	envMetricsToken = "METRICS_TOKEN"

	// AppEnvLocal is a developer machine: human-readable logs, no crash reporting expected.
	AppEnvLocal = "local"
	// AppEnvStaging mirrors production.
	AppEnvStaging = "staging"
	// AppEnvProduction moves real member money.
	AppEnvProduction = "production"

	// LogFormatText is slog's key=value output, for a terminal.
	LogFormatText = "text"
	// LogFormatJSON is one JSON object per line, for log shippers.
	LogFormatJSON = "json"

	// DefaultMetricsAddr keeps /metrics on loopback: reachable by a local scraper or an
	// SSH tunnel, never from the network.
	DefaultMetricsAddr = "127.0.0.1:9090"
	// MetricsAddrOff disables the metrics listener.
	MetricsAddrOff = "off"

	// minMetricsTokenLen rejects a token short enough to guess.
	minMetricsTokenLen = 24
)

// Observability is what logging, crash reporting and metrics need before the rest of the
// config is usable, so a boot failure is itself logged and reported.
//
//   - APP_ENV: local (default), staging or production. Tags crash reports and picks the log format.
//   - APP_RELEASE: release tag for crash reports (a git SHA). Unset uses the VCS revision
//     stamped into the binary by `go build`.
//   - SENTRY_DSN: enables Sentry crash and error reporting. Unset = reporting off.
//   - LOG_FORMAT: text or json. Unset = text when APP_ENV=local, json everywhere else.
//   - METRICS_ADDR: listen address of the Prometheus /metrics server (default 127.0.0.1:9090,
//     "off" disables it). It is a separate listener so the public API port never serves it.
//   - METRICS_TOKEN: bearer token /metrics requires. Mandatory when METRICS_ADDR is not loopback.
type Observability struct {
	AppEnv       string
	Release      string
	SentryDSN    string
	LogFormat    string
	MetricsAddr  string
	MetricsToken string
}

// LoadObservability reads the observability settings from the process environment.
func LoadObservability() (Observability, error) {
	o := Observability{
		AppEnv:       strings.ToLower(strings.TrimSpace(os.Getenv(envAppEnv))),
		Release:      strings.TrimSpace(os.Getenv(envAppRelease)),
		SentryDSN:    strings.TrimSpace(os.Getenv(envSentryDSN)),
		LogFormat:    strings.ToLower(strings.TrimSpace(os.Getenv(envLogFormat))),
		MetricsAddr:  strings.TrimSpace(os.Getenv(envMetricsAddr)),
		MetricsToken: strings.TrimSpace(os.Getenv(envMetricsToken)),
	}

	switch o.AppEnv {
	case "":
		o.AppEnv = AppEnvLocal
	case AppEnvLocal, AppEnvStaging, AppEnvProduction:
	default:
		return Observability{}, fmt.Errorf("%s must be %s, %s or %s, got %q", envAppEnv, AppEnvLocal, AppEnvStaging, AppEnvProduction, o.AppEnv)
	}

	switch o.LogFormat {
	case "":
		o.LogFormat = LogFormatJSON
		if o.AppEnv == AppEnvLocal {
			o.LogFormat = LogFormatText
		}
	case LogFormatText, LogFormatJSON:
	default:
		return Observability{}, fmt.Errorf("%s must be %s or %s, got %q", envLogFormat, LogFormatText, LogFormatJSON, o.LogFormat)
	}

	if err := o.resolveMetrics(); err != nil {
		return Observability{}, err
	}
	return o, nil
}

// MetricsEnabled reports whether the metrics listener should start.
func (o Observability) MetricsEnabled() bool {
	return o.MetricsAddr != MetricsAddrOff
}

func (o *Observability) resolveMetrics() error {
	if o.MetricsAddr == "" {
		o.MetricsAddr = DefaultMetricsAddr
	}
	if strings.EqualFold(o.MetricsAddr, MetricsAddrOff) {
		o.MetricsAddr = MetricsAddrOff
		return nil
	}
	if o.MetricsToken != "" && len(o.MetricsToken) < minMetricsTokenLen {
		return fmt.Errorf("%s must be at least %d characters", envMetricsToken, minMetricsTokenLen)
	}
	host, _, err := net.SplitHostPort(o.MetricsAddr)
	if err != nil {
		return fmt.Errorf("%s must be host:port or %q: %w", envMetricsAddr, MetricsAddrOff, err)
	}
	// Metrics expose money-path volumes and queue depths; an empty host binds every interface.
	if !IsLoopbackHost(host) && o.MetricsToken == "" {
		return fmt.Errorf("%s=%q is not loopback, so %s is required", envMetricsAddr, o.MetricsAddr, envMetricsToken)
	}
	return nil
}
