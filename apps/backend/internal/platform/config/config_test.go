package config_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/platform/config"
)

func required() []string {
	return []string{
		"MONACO_ENV=local",
		"DATABASE_URL=postgres://monaco@localhost:54322/monaco",
		"NATS_URL=nats://localhost:4222",
	}
}

func apnsKeys() []string {
	return []string{"APNS_KEY_P8=p8-key", "APNS_KEY_ID=key-id", "APNS_TEAM_ID=team-id"}
}

func TestLoadFillsDefaultsFromTheRFC(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(append(required(), "PATH=/usr/bin", "HOME=/home/monaco"))
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{
		Env:    config.EnvLocal,
		HTTP:   config.HTTP{Addr: ":8080", MaxBodyBytes: 1 << 20},
		Worker: config.Worker{HealthAddr: ":8081"},
		DB:     config.DB{URL: "postgres://monaco@localhost:54322/monaco", MaxConns: 11},
		NATS:   config.NATS{URL: "nats://localhost:4222"},
		Bus:    config.Bus{APIRelay: true},
		OTel:   config.OTel{ServiceName: "monaco"},
		Timeouts: config.Timeouts{
			RPC:             5 * time.Second,
			Privy:           10 * time.Second,
			APNs:            10 * time.Second,
			JupiterQuote:    5 * time.Second,
			JupiterExecute:  2 * time.Minute,
			XStocks:         15 * time.Second,
			Tessera:         15 * time.Second,
			PreStocks:       15 * time.Second,
			PostHog:         3 * time.Second,
			Storage:         10 * time.Second,
			HTTPServerRead:  10 * time.Second,
			HTTPServerWrite: 30 * time.Second,
			Shutdown:        10 * time.Second,
		},
		Jupiter: config.Jupiter{
			SwapBaseURL:  "https://api.jup.ag/swap/v2",
			PriceBaseURL: "https://api.jup.ag/price/v3",
		},
		XStocks:   config.XStocks{BaseURL: "https://api.xstocks.fi"},
		Tessera:   config.Tessera{BaseURL: "https://rest-api.tessera.pe"},
		PreStocks: config.PreStocks{BaseURL: "https://prestocks.com"},
		Market:    config.Market{PricePollInterval: 2 * time.Minute},
		Identity:  config.Identity{NudgesInterval: 24 * time.Hour},
		Funding:   config.Funding{DepositPollInterval: 30 * time.Second, DepositRPCRate: 20},
		Privy:     config.Privy{BaseURL: "https://api.privy.io"},
		APNs:      config.APNs{Topic: "com.monaco.app"},
		PostHog:   config.PostHog{Host: "https://us.i.posthog.com"},
		Solana: config.Solana{
			RPCURL:   "https://api.mainnet-beta.solana.com",
			USDCMint: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load = %+v, want %+v", cfg, want)
	}
}

func TestLoadDefaultPoolLeavesConnectionFreeAfterWorkerPollersHoldTheirLocks(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(append(required(), "PATH=/usr/bin", "HOME=/home/monaco"))
	if err != nil {
		t.Fatal(err)
	}
	const workerPollers = 10
	if cfg.DB.MaxConns <= workerPollers {
		t.Fatalf("DB.MaxConns = %d, want more than %d worker pollers", cfg.DB.MaxConns, workerPollers)
	}
}

func TestLoadReadsEveryKey(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load([]string{
		"MONACO_ENV=production",
		"MONACO_HTTP_ADDR=127.0.0.1:9000",
		"MONACO_HTTP_MAX_BODY_BYTES=4096",
		"TRUST_PROXY_HEADERS=true",
		"MONACO_WORKER_HEALTH_ADDR=127.0.0.1:9001",
		"DATABASE_URL=postgres://prod",
		"MONACO_DB_MAX_CONNS=40",
		"NATS_URL=nats://prod:4222",
		"OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp.example",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic abc",
		"OTEL_SERVICE_NAME=monaco-api",
		"MONACO_DEV_TOKEN_KEY=dev-secret",
		"MONACO_TIMEOUT_RPC=1s",
		"MONACO_TIMEOUT_PRIVY=2s",
		"MONACO_TIMEOUT_APNS=8s",
		"MONACO_TIMEOUT_JUPITER_QUOTE=3s",
		"MONACO_TIMEOUT_JUPITER_EXECUTE=4m",
		"MONACO_TIMEOUT_XSTOCKS=8s",
		"MONACO_TIMEOUT_TESSERA=9s",
		"MONACO_TIMEOUT_PRESTOCKS=11s",
		"MONACO_TIMEOUT_POSTHOG=2500ms",
		"MONACO_TIMEOUT_HTTP_SERVER_READ=5s",
		"MONACO_TIMEOUT_HTTP_SERVER_WRITE=6s",
		"MONACO_TIMEOUT_SHUTDOWN=7s",
		"MONACO_JUPITER_SWAP_BASE_URL=http://fakes/jupiter/swap/v2",
		"MONACO_JUPITER_PRICE_BASE_URL=http://fakes/jupiter/price/v3",
		"JUPITER_API_KEY=jup-secret",
		"XSTOCKS_BASE_URL=http://fakes/xstocks",
		"TESSERA_API_BASE_URL=http://fakes/tessera",
		"PRESTOCKS_API_BASE_URL=http://fakes/prestocks",
		"MARKET_PRICE_POLL_INTERVAL=1s",
		"IDENTITY_NUDGES_INTERVAL=2s",
		"FUNDING_DEPOSIT_POLL_INTERVAL=2s",
		"FUNDING_DEPOSIT_RPC_RATE=12",
		"PRIVY_APP_ID=app-id",
		"PRIVY_APP_SECRET=app-secret",
		"PRIVY_VERIFICATION_KEY=verification-pem",
		"PRIVY_AUTHORIZATION_PRIVATE_KEY=wallet-auth:key",
		"PRIVY_AUTHORIZATION_KEY_ID=quorum-id",
		"PRIVY_WEBHOOK_SECRET=whsec_x",
		"PRIVY_BASE_URL=http://fakes/privy",
		"SOLANA_RPC_URL=http://fakes/rpc",
		"SOLANA_USDC_MINT=mint",
		"RELAYER_PRIVATE_KEY=relayer-key",
		"APNS_KEY_P8=p8-key",
		"APNS_KEY_ID=key-id",
		"APNS_TEAM_ID=team-id",
		"APNS_TOPIC=com.example.app",
		"POSTHOG_API_KEY=ph-secret",
		"POSTHOG_HOST=http://fakes/posthog",
		"SUPABASE_URL=http://127.0.0.1:54321",
		"SUPABASE_SERVICE_ROLE_KEY=sb-secret",
		"MONACO_TIMEOUT_STORAGE=4s",
		"MONACO_FAULTPOINT=before-commit",
		"MONACO_BUS_ACK_WAIT=100ms",
		"MONACO_BUS_API_RELAY=off",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{
		Env:    config.EnvProduction,
		HTTP:   config.HTTP{Addr: "127.0.0.1:9000", MaxBodyBytes: 4096, TrustProxyHeaders: true},
		Worker: config.Worker{HealthAddr: "127.0.0.1:9001"},
		DB:     config.DB{URL: "postgres://prod", MaxConns: 40},
		NATS:   config.NATS{URL: "nats://prod:4222"},
		Bus:    config.Bus{AckWait: 100 * time.Millisecond},
		OTel: config.OTel{
			Endpoint:    "https://otlp.example",
			Headers:     "Authorization=Basic abc",
			ServiceName: "monaco-api",
		},
		Auth: config.Auth{DevTokenKey: "dev-secret"},
		Timeouts: config.Timeouts{
			RPC:             time.Second,
			Privy:           2 * time.Second,
			APNs:            8 * time.Second,
			JupiterQuote:    3 * time.Second,
			JupiterExecute:  4 * time.Minute,
			XStocks:         8 * time.Second,
			Tessera:         9 * time.Second,
			PreStocks:       11 * time.Second,
			PostHog:         2500 * time.Millisecond,
			Storage:         4 * time.Second,
			HTTPServerRead:  5 * time.Second,
			HTTPServerWrite: 6 * time.Second,
			Shutdown:        7 * time.Second,
		},
		Jupiter: config.Jupiter{
			SwapBaseURL:  "http://fakes/jupiter/swap/v2",
			PriceBaseURL: "http://fakes/jupiter/price/v3",
			APIKey:       "jup-secret",
		},
		XStocks:   config.XStocks{BaseURL: "http://fakes/xstocks"},
		Tessera:   config.Tessera{BaseURL: "http://fakes/tessera"},
		PreStocks: config.PreStocks{BaseURL: "http://fakes/prestocks"},
		Market:    config.Market{PricePollInterval: time.Second},
		Identity:  config.Identity{NudgesInterval: 2 * time.Second},
		Funding:   config.Funding{DepositPollInterval: 2 * time.Second, DepositRPCRate: 12},
		Privy: config.Privy{
			AppID: "app-id", AppSecret: "app-secret", VerificationKey: "verification-pem",
			AuthorizationPrivateKey: "wallet-auth:key", AuthorizationKeyID: "quorum-id",
			WebhookSecret: "whsec_x", BaseURL: "http://fakes/privy",
		},
		Solana:     config.Solana{RPCURL: "http://fakes/rpc", USDCMint: "mint"},
		Relayer:    config.Relayer{PrivateKey: "relayer-key"},
		APNs:       config.APNs{KeyP8: "p8-key", KeyID: "key-id", TeamID: "team-id", Topic: "com.example.app"},
		PostHog:    config.PostHog{APIKey: "ph-secret", Host: "http://fakes/posthog"},
		Supabase:   config.Supabase{URL: "http://127.0.0.1:54321", ServiceRoleKey: "sb-secret"},
		Faultpoint: "before-commit",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load = %+v, want %+v", cfg, want)
	}
}

func TestLoadAcceptsEveryEnv(t *testing.T) {
	t.Parallel()
	for _, env := range []config.Env{config.EnvLocal, config.EnvTest, config.EnvStaging, config.EnvProduction} {
		environ := append(append(required(), apnsKeys()...), "MONACO_ENV="+string(env))
		if env == config.EnvProduction {
			environ = append(environ, "POSTHOG_API_KEY=ph-key")
		}
		cfg, err := config.Load(environ)
		if err != nil {
			t.Fatalf("MONACO_ENV=%s: %v", env, err)
		}
		if cfg.Env != env {
			t.Fatalf("Env = %q, want %q", cfg.Env, env)
		}
	}
}

func TestEnvDeployedIsStagingAndProductionOnly(t *testing.T) {
	t.Parallel()
	for env, want := range map[config.Env]bool{
		config.EnvLocal:      false,
		config.EnvTest:       false,
		config.EnvStaging:    true,
		config.EnvProduction: true,
		"":                   false,
	} {
		if got := env.Deployed(); got != want {
			t.Errorf("Env(%q).Deployed() = %v, want %v", env, got, want)
		}
	}
}

func TestLoadFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		environ []string
		want    string
	}{
		{
			name:    "nothing set names every required key",
			environ: []string{"PATH=/usr/bin"},
			want:    "config.Load: invalid_input: missing MONACO_ENV, DATABASE_URL, NATS_URL",
		},
		{
			name:    "missing database and nats in one error",
			environ: []string{"MONACO_ENV=local"},
			want:    "config.Load: invalid_input: missing DATABASE_URL, NATS_URL",
		},
		{
			name:    "empty value counts as missing",
			environ: append(required(), "DATABASE_URL="),
			want:    "config.Load: invalid_input: missing DATABASE_URL",
		},
		{
			name:    "unknown MONACO key",
			environ: append(required(), "MONACO_FOO=1", "MONACO_BAR", "FOO=1"),
			want:    "config.Load: invalid_input: unknown MONACO_BAR, MONACO_FOO",
		},
		{
			name:    "env outside the four names",
			environ: append(required(), "MONACO_ENV=prod"),
			want:    "config.Load: invalid_input: invalid MONACO_ENV (local, test, staging or production)",
		},
		{
			name: "malformed numbers and durations",
			environ: append(required(),
				"MONACO_DB_MAX_CONNS=0",
				"MONACO_TIMEOUT_RPC=5",
				"MONACO_TIMEOUT_PRIVY=-1s",
				"MONACO_TIMEOUT_SHUTDOWN=0s",
			),
			want: "config.Load: invalid_input: invalid MONACO_DB_MAX_CONNS (positive integer), " +
				"MONACO_TIMEOUT_RPC (positive duration like 5s), MONACO_TIMEOUT_PRIVY (positive duration like 5s), " +
				"MONACO_TIMEOUT_SHUTDOWN (positive duration like 5s)",
		},
		{
			name:    "bus knobs",
			environ: append(required(), "MONACO_BUS_ACK_WAIT=0s", "MONACO_BUS_API_RELAY=maybe"),
			want: "config.Load: invalid_input: invalid MONACO_BUS_ACK_WAIT (empty or a positive duration like 100ms), " +
				"MONACO_BUS_API_RELAY (on or off)",
		},
		{
			name:    "staging without the APNs key names every missing key",
			environ: append(required(), "MONACO_ENV=staging"),
			want:    "config.Load: invalid_input: missing APNS_KEY_P8, APNS_KEY_ID, APNS_TEAM_ID",
		},
		{
			name: "production with a partial APNs key",
			environ: append(
				required(),
				"MONACO_ENV=production",
				"POSTHOG_API_KEY=ph-key",
				"APNS_KEY_ID=key-id",
			),
			want: "config.Load: invalid_input: missing APNS_KEY_P8, APNS_TEAM_ID",
		},
		{
			name:    "a local APNs key needs its ids",
			environ: append(required(), "APNS_KEY_P8=p8-key"),
			want:    "config.Load: invalid_input: missing APNS_KEY_ID, APNS_TEAM_ID",
		},
		{
			name: "production refuses an APNs base URL",
			environ: append(
				append(required(), apnsKeys()...),
				"MONACO_ENV=production",
				"POSTHOG_API_KEY=ph-key",
				"APNS_BASE_URL=http://fakes/apns",
			),
			want: "config.Load: invalid_input: invalid APNS_BASE_URL (not allowed in production)",
		},
		{
			name: "missing keys and a production base URL in one error",
			environ: append(
				required(),
				"MONACO_ENV=production",
				"POSTHOG_API_KEY=ph-key",
				"APNS_BASE_URL=http://fakes/apns",
			),
			want: "config.Load: invalid_input: missing APNS_KEY_P8, APNS_KEY_ID, APNS_TEAM_ID; " +
				"invalid APNS_BASE_URL (not allowed in production)",
		},
		{
			name:    "APNs timeout that is not a positive duration",
			environ: append(required(), "MONACO_TIMEOUT_APNS=0s"),
			want:    "config.Load: invalid_input: invalid MONACO_TIMEOUT_APNS (positive duration like 5s)",
		},
		{
			name:    "production without a PostHog key",
			environ: append(append(required(), apnsKeys()...), "MONACO_ENV=production"),
			want:    "config.Load: invalid_input: missing POSTHOG_API_KEY",
		},
		{
			name:    "an empty PostHog key counts as missing in production",
			environ: append(append(required(), apnsKeys()...), "MONACO_ENV=production", "POSTHOG_API_KEY="),
			want:    "config.Load: invalid_input: missing POSTHOG_API_KEY",
		},
		{
			name:    "production names every missing key in one error",
			environ: []string{"MONACO_ENV=production", "NATS_URL=nats://x"},
			want: "config.Load: invalid_input: missing DATABASE_URL, APNS_KEY_P8, APNS_KEY_ID, APNS_TEAM_ID, " +
				"POSTHOG_API_KEY",
		},
		{
			name:    "trust proxy headers not a boolean",
			environ: append(required(), "TRUST_PROXY_HEADERS=render"),
			want:    "config.Load: invalid_input: invalid TRUST_PROXY_HEADERS (true or false)",
		},
		{
			name:    "max conns past int32",
			environ: append(required(), "MONACO_DB_MAX_CONNS=2147483648"),
			want:    "config.Load: invalid_input: invalid MONACO_DB_MAX_CONNS (positive integer)",
		},
		{
			name:    "every kind of problem at once",
			environ: []string{"MONACO_ENV=local", "NATS_URL=nats://x", "MONACO_FOO=1", "MONACO_DB_MAX_CONNS=x"},
			want: "config.Load: invalid_input: missing DATABASE_URL; unknown MONACO_FOO; " +
				"invalid MONACO_DB_MAX_CONNS (positive integer)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(tt.environ)
			if err == nil {
				t.Fatalf("Load = %+v, want error %q", cfg, tt.want)
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
			if target := (*errs.Error)(nil); !errors.As(err, &target) || errs.CodeOf(err) != errs.CodeInvalidInput {
				t.Fatalf("error %v is not an *errs.Error with code invalid_input", err)
			}
			if !reflect.DeepEqual(cfg, config.Config{}) {
				t.Fatalf("Load returned %+v with an error, want the zero Config", cfg)
			}
		})
	}
}

func TestLoadErrorNeverEchoesAValue(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-value"
	_, err := config.Load([]string{"DATABASE_URL=" + secret, "MONACO_ENV=" + secret, "MONACO_TIMEOUT_RPC=" + secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error %v echoes a value", err)
	}
}

func TestRedactedHidesSecretsAndShowsTheRest(t *testing.T) {
	t.Parallel()
	secrets := map[string]string{
		"DATABASE_URL":                    "postgres://db-secret@host/db",
		"NATS_URL":                        "nats://token-secret@host:4222",
		"OTEL_EXPORTER_OTLP_HEADERS":      "Authorization=Basic header-secret",
		"MONACO_DEV_TOKEN_KEY":            "dev-token-secret",
		"JUPITER_API_KEY":                 "jup-secret",
		"PRIVY_APP_SECRET":                "privy-app-secret",
		"PRIVY_AUTHORIZATION_PRIVATE_KEY": "wallet-auth:privy-auth-secret",
		"PRIVY_WEBHOOK_SECRET":            "webhook-signing-secret",
		"SOLANA_RPC_URL":                  "https://rpc.example/rpc-secret",
		"RELAYER_PRIVATE_KEY":             "relayer-secret",
		"APNS_KEY_P8":                     "p8-secret",
		"POSTHOG_API_KEY":                 "ph-api-secret",
		"SUPABASE_SERVICE_ROLE_KEY":       "supabase-service-secret",
	}
	environ := make([]string, 0, 2+len(secrets))
	environ = append(environ, "MONACO_ENV=staging", "MONACO_TIMEOUT_JUPITER_EXECUTE=90s",
		"APNS_KEY_ID=key-id", "APNS_TEAM_ID=team-id")
	for k, v := range secrets {
		environ = append(environ, k+"="+v)
	}
	cfg, err := config.Load(environ)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Redacted()
	tests := []struct {
		key  string
		want string
	}{
		{"DATABASE_URL", "***"},
		{"NATS_URL", "***"},
		{"OTEL_EXPORTER_OTLP_HEADERS", "***"},
		{"MONACO_DEV_TOKEN_KEY", "***"},
		{"JUPITER_API_KEY", "***"},
		{"MONACO_JUPITER_SWAP_BASE_URL", "https://api.jup.ag/swap/v2"},
		{"MONACO_JUPITER_PRICE_BASE_URL", "https://api.jup.ag/price/v3"},
		{"MONACO_ENV", "staging"},
		{"MONACO_HTTP_ADDR", ":8080"},
		{"MONACO_HTTP_MAX_BODY_BYTES", "1048576"},
		{"MONACO_WORKER_HEALTH_ADDR", ":8081"},
		{"MONACO_DB_MAX_CONNS", "11"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", ""},
		{"OTEL_SERVICE_NAME", "monaco"},
		{"MONACO_TIMEOUT_RPC", "5s"},
		{"MONACO_TIMEOUT_PRIVY", "10s"},
		{"MONACO_TIMEOUT_JUPITER_QUOTE", "5s"},
		{"MONACO_TIMEOUT_JUPITER_EXECUTE", "1m30s"},
		{"MONACO_TIMEOUT_XSTOCKS", "15s"},
		{"XSTOCKS_BASE_URL", "https://api.xstocks.fi"},
		{"MONACO_TIMEOUT_TESSERA", "15s"},
		{"TESSERA_API_BASE_URL", "https://rest-api.tessera.pe"},
		{"MONACO_TIMEOUT_PRESTOCKS", "15s"},
		{"PRESTOCKS_API_BASE_URL", "https://prestocks.com"},
		{"MARKET_PRICE_POLL_INTERVAL", "2m0s"},
		{"IDENTITY_NUDGES_INTERVAL", "24h0m0s"},
		{"FUNDING_DEPOSIT_POLL_INTERVAL", "30s"},
		{"FUNDING_DEPOSIT_RPC_RATE", "20"},
		{"MONACO_TIMEOUT_HTTP_SERVER_READ", "10s"},
		{"MONACO_TIMEOUT_HTTP_SERVER_WRITE", "30s"},
		{"MONACO_TIMEOUT_SHUTDOWN", "10s"},
		{"PRIVY_APP_SECRET", "***"},
		{"PRIVY_AUTHORIZATION_PRIVATE_KEY", "***"},
		{"PRIVY_WEBHOOK_SECRET", "***"},
		{"SOLANA_RPC_URL", "***"},
		{"RELAYER_PRIVATE_KEY", "***"},
		{"APNS_KEY_P8", "***"},
		{"APNS_KEY_ID", "key-id"},
		{"APNS_TEAM_ID", "team-id"},
		{"APNS_TOPIC", "com.monaco.app"},
		{"APNS_BASE_URL", ""},
		{"MONACO_TIMEOUT_APNS", "10s"},
		{"POSTHOG_API_KEY", "***"},
		{"POSTHOG_HOST", "https://us.i.posthog.com"},
		{"MONACO_TIMEOUT_POSTHOG", "3s"},
		{"SUPABASE_URL", ""},
		{"SUPABASE_SERVICE_ROLE_KEY", "***"},
		{"MONACO_TIMEOUT_STORAGE", "10s"},
		{"PRIVY_APP_ID", ""},
		{"PRIVY_VERIFICATION_KEY", ""},
		{"PRIVY_AUTHORIZATION_KEY_ID", ""},
		{"PRIVY_BASE_URL", "https://api.privy.io"},
		{"SOLANA_USDC_MINT", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},
		{"MONACO_FAULTPOINT", ""},
		{"MONACO_BUS_ACK_WAIT", ""},
		{"MONACO_BUS_API_RELAY", "on"},
		{"TRUST_PROXY_HEADERS", "false"},
	}
	if len(got) != len(tests) {
		t.Fatalf("Redacted has %d keys, want %d: %v", len(got), len(tests), got)
	}
	for _, tt := range tests {
		if got[tt.key] != tt.want {
			t.Errorf("Redacted()[%s] = %q, want %q", tt.key, got[tt.key], tt.want)
		}
	}
	for key, shown := range got {
		for _, secret := range secrets {
			if strings.Contains(shown, secret) {
				t.Errorf("Redacted()[%s] leaks a secret", key)
			}
		}
	}
}

func TestLoad_busKnobsRoundTripThroughRedacted(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(append(required(), "MONACO_BUS_ACK_WAIT=100ms", "MONACO_BUS_API_RELAY=off"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Redacted(); got["MONACO_BUS_ACK_WAIT"] != "100ms" || got["MONACO_BUS_API_RELAY"] != "off" {
		t.Fatalf("Redacted = %v, want the ack wait and relay switch as set", got)
	}
}

func TestTestDBURLDefaultsToTheTestContainer(t *testing.T) {
	t.Parallel()
	if got := config.TestDBURL(required()); got != config.DefaultTestDBURL || !strings.Contains(got, ":54323/") {
		t.Fatalf("TestDBURL without TEST_DATABASE_URL = %q", got)
	}
	if got := config.TestDBURL(append(required(), "TEST_DATABASE_URL=")); got != config.DefaultTestDBURL {
		t.Fatalf("TestDBURL with an empty TEST_DATABASE_URL = %q", got)
	}
	custom := "postgres://ci@127.0.0.1:54323/ci"
	if got := config.TestDBURL(append(required(), "TEST_DATABASE_URL="+custom)); got != custom {
		t.Fatalf("TestDBURL = %q, want %q", got, custom)
	}
}
