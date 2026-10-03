package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/platform/bus"
	"github.com/monaco/monaco/apps/backend/internal/platform/config"
	"github.com/monaco/monaco/apps/backend/internal/platform/module"
	"github.com/monaco/monaco/apps/backend/internal/testkit"
	"github.com/monaco/monaco/apps/backend/internal/testkit/fakes"
)

func TestMain(m *testing.M) {
	testkit.Main(m, testkit.WithChild(main), testkit.WithNATS())
}

func bootEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	url := testkit.StandaloneNATS(t)
	conn, err := bus.Connect(t.Context(), config.NATS{URL: url}, bus.ProcessMonacoctl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn.Close(t.Context())
	upstreams := httptest.NewServer(fakes.New())
	t.Cleanup(upstreams.Close)
	return append(
		[]string{
			"MONACO_ENV=test", "DATABASE_URL=" + testkit.DB(t).Config().ConnString(), "NATS_URL=" + url,
			"XSTOCKS_BASE_URL=" + upstreams.URL + "/xstocks",
			"TESSERA_API_BASE_URL=" + upstreams.URL + "/tessera",
			"PRESTOCKS_API_BASE_URL=" + upstreams.URL + "/prestocks",
			"MONACO_JUPITER_PRICE_BASE_URL=" + upstreams.URL + "/jupiter/price/v3",
			"SOLANA_RPC_URL=" + upstreams.URL + "/rpc/",
			"FUNDING_DEPOSIT_POLL_INTERVAL=1s",
		},
		extra...)
}

func TestMain_servesHealthzUntilSIGTERMThenExitsZero(t *testing.T) {
	t.Parallel()
	p := testkit.StartMain(t, bootEnv(t, "MONACO_WORKER_HEALTH_ADDR=127.0.0.1:0"))
	want := "nats ok\ndb ok\npoller:platform.retention ok\npoller:cabal.invite_expiry ok\npoller:funding.deposits ok\n" +
		"poller:governance.proposal_expiry ok\npoller:identity.nudges ok\n" +
		"poller:identity.photo_purges ok\npoller:market.catalog ok\n" +
		"poller:market.prices ok\npoller:market.retention ok\npoller:trading.swap_sweeper ok\n"
	waitUntil(t, "a healthy worker", func() bool {
		code, body := testkit.Get(t, "http://"+p.Addr+"/healthz")
		return code == http.StatusOK && body == want
	})
	if stderr, err := p.Terminate(); err != nil {
		t.Fatalf("worker after SIGTERM: %v\n%s", err, stderr)
	}
}

func TestMain_exitsOneAndLogsWhyWhenConfigIsMissing(t *testing.T) {
	t.Parallel()
	out, err := testkit.MainCommand(t, nil).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 ||
		!strings.Contains(string(out), `"msg":"boot.stopped"`) || !strings.Contains(string(out), "missing MONACO_ENV") {
		t.Fatalf("worker without config = %v\n%s", err, out)
	}
}

func TestRun_refusesToBootWithoutRequiredConfig(t *testing.T) {
	t.Parallel()
	err := run(t.Context(), io.Discard, []string{"PATH=/usr/bin"}, noop.NewMeterProvider(), &module.Registry{})
	want := "config.Load: invalid_input: missing MONACO_ENV, DATABASE_URL, NATS_URL"
	if err == nil || err.Error() != want || errs.CodeOf(err) != errs.CodeInvalidInput {
		t.Fatalf("run = %v, want %q", err, want)
	}
}

func TestRun_refusesToBootInProductionWithoutAPostHogKey(t *testing.T) {
	t.Parallel()
	err := run(t.Context(), io.Discard, append([]string{
		"MONACO_ENV=production", "DATABASE_URL=postgres://localhost/monaco", "NATS_URL=nats://localhost:4222",
	}, testkit.APNsEnv()...), noop.NewMeterProvider(), &module.Registry{})
	want := "config.Load: invalid_input: missing POSTHOG_API_KEY"
	if err == nil || err.Error() != want || errs.CodeOf(err) != errs.CodeInvalidInput {
		t.Fatalf("run = %v, want %q", err, want)
	}
}

func TestRun_refusesToBootWithMalformedOTelEndpoint(t *testing.T) {
	t.Parallel()
	err := run(t.Context(), io.Discard, []string{
		"MONACO_ENV=test", "DATABASE_URL=postgres://localhost/monaco", "NATS_URL=nats://localhost:4222",
		"OTEL_EXPORTER_OTLP_ENDPOINT=collector:4318",
	}, noop.NewMeterProvider(), &module.Registry{})
	if errs.CodeOf(err) != errs.CodeInvalidInput || !strings.Contains(err.Error(), "observability.Setup") {
		t.Fatalf("run = %v, want invalid_input from observability.Setup", err)
	}
}

func TestRun_reportsAnAddressItCannotListenOn(t *testing.T) {
	t.Parallel()
	err := run(
		t.Context(),
		io.Discard,
		bootEnv(t, "MONACO_WORKER_HEALTH_ADDR=256.0.0.1:1"),
		noop.NewMeterProvider(),
		&module.Registry{},
	)
	if err == nil || !strings.Contains(err.Error(), "listen on 256.0.0.1:1") {
		t.Fatalf("run = %v, want a listen error", err)
	}
}

func TestRun_refusesToBootNamingTheFirstGaugeExportThatFailed(t *testing.T) {
	t.Parallel()
	for prefix, op := range map[string]string{
		"monaco_events_":       "bus.Relay.ExportBacklogGauges",
		"monaco_bus_account_":  "bus.ExportAccountGauges",
		"monaco_bus_consumer_": "bus.NewRegistry",
		"poller_errors_":       "poller.NewRunner",
	} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			err := run(t.Context(), io.Discard, bootEnv(t, "MONACO_WORKER_HEALTH_ADDR=127.0.0.1:0"),
				testkit.FailingGauges{Prefix: prefix}, &module.Registry{})
			if errs.CodeOf(err) != errs.CodeInternal || !strings.HasPrefix(err.Error(), op+": ") {
				t.Fatalf("run with %s gauges failing = %v, want internal from %s", prefix, err, op)
			}
		})
	}
}

func TestRun_refusesToBootWithAnUnknownFaultpoint(t *testing.T) {
	t.Parallel()
	err := run(t.Context(), io.Discard, []string{
		"MONACO_ENV=test", "DATABASE_URL=postgres://localhost/monaco", "NATS_URL=nats://localhost:4222",
		"MONACO_FAULTPOINT=after-everything",
	}, noop.NewMeterProvider(), &module.Registry{})
	if err == nil || err.Error() != "faultpoint.Configure: invalid_input" {
		t.Fatalf("run = %v, want invalid_input from faultpoint.Configure", err)
	}
}
