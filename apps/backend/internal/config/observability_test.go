package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_missingPrivyVerificationKey_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRIVY_VERIFICATION_KEY", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil || !strings.Contains(err.Error(), "PRIVY_VERIFICATION_KEY is required") {
		t.Fatalf("err = %v, want PRIVY_VERIFICATION_KEY is required", err)
	}
}

func TestLoad_privyVerificationKey_rejectsWhatCannotVerifyES256(t *testing.T) {
	cases := map[string][2]string{
		"not pem":   {"abc123", "not a PEM EC public key"},
		"rsa key":   {"-----BEGIN PUBLIC KEY-----\nMFwwDQYJKoZIhvcNAQEBBQADSwAwSAJBALocPoQ9MCezRZ7QHiXY0pevxDvN3vnf\naWUCmZsb2WUrKDfyjtgpEpoAUOcx9mPHWuX4gqb1sER+R3BIFaodOv0CAwEAAQ==\n-----END PUBLIC KEY-----", "not a PEM EC public key"},
		"p-384 key": {"-----BEGIN PUBLIC KEY-----\nMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEGVcuI0jcA9f8P8b+uKu9IYNkqvrnfwrK\n40YLF3IN/i+g0uWJnciXLa9uY7TV/KreDLZ6IiMUV6UUT1w32ESWClvQ6g3aBQMA\nlvxE0FCHHafx1ZnZUHkyOxa/2ujHDToj\n-----END PUBLIC KEY-----", "must be a P-256 key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			clearConfigEnv(t)
			setValidConfigEnv(t)
			t.Setenv("PRIVY_VERIFICATION_KEY", tc[0])

			// Act
			_, err := Load()

			// Assert
			if err == nil || !strings.Contains(err.Error(), "PRIVY_VERIFICATION_KEY") || !strings.Contains(err.Error(), tc[1]) {
				t.Fatalf("err = %v, want PRIVY_VERIFICATION_KEY %s", err, tc[1])
			}
		})
	}
}

func TestLoad_privyVerificationKey_acceptsSingleLineEnvForm(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRIVY_VERIFICATION_KEY", strings.ReplaceAll(TestPrivyVerificationKeyPEM(), "\n", `\n`))

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PrivyVerificationKey == nil {
		t.Fatal("PrivyVerificationKey = nil")
	}
}

func TestLoad_solanaRPCURL_overridesPublicEndpoint(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("SOLANA_RPC_URL", " https://rpc.example.com/?api-key=k ")

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.SolanaRPCEndpoint(); got != "https://rpc.example.com/?api-key=k" {
		t.Fatalf("SolanaRPCEndpoint = %q", got)
	}
}

func TestLoad_solanaRPCURL_unsetFallsBackToPublicCluster(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.SolanaRPCEndpoint(); got != "https://api.mainnet-beta.solana.com" {
		t.Fatalf("SolanaRPCEndpoint = %q", got)
	}
}

func TestLoad_solanaRPCURL_malformed_failsWithoutEchoingTheKey(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("SOLANA_RPC_URL", "rpc.example.com/?api-key=topsecret")

	// Act
	_, err := Load()

	// Assert
	if err == nil || !strings.Contains(err.Error(), "SOLANA_RPC_URL") {
		t.Fatalf("err = %v, want a SOLANA_RPC_URL error", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaks the RPC key: %v", err)
	}
}

func TestLoad_dbPool_defaultsAreBounded(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := DBPool{MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: 30 * time.Minute, ConnMaxIdleTime: 5 * time.Minute}
	if cfg.DBPool != want {
		t.Fatalf("DBPool = %+v, want %+v", cfg.DBPool, want)
	}
}

func TestLoad_dbPool_overridesAndClampsIdleToOpen(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "4")
	t.Setenv("DB_CONN_MAX_LIFETIME", "10m")

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DBPool.MaxOpenConns != 4 || cfg.DBPool.MaxIdleConns != 4 || cfg.DBPool.ConnMaxLifetime != 10*time.Minute {
		t.Fatalf("DBPool = %+v", cfg.DBPool)
	}
}

func TestLoad_dbPool_rejectsUnboundedOrMalformedValues(t *testing.T) {
	cases := map[string][2]string{
		"zero open conns means unlimited": {"DB_MAX_OPEN_CONNS", "0"},
		"negative idle":                   {"DB_MAX_IDLE_CONNS", "-1"},
		"lifetime without unit":           {"DB_CONN_MAX_LIFETIME", "30"},
		"zero idle time":                  {"DB_CONN_MAX_IDLE_TIME", "0s"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			clearConfigEnv(t)
			setValidConfigEnv(t)
			t.Setenv(tc[0], tc[1])

			// Act
			_, err := Load()

			// Assert
			if err == nil || !strings.Contains(err.Error(), tc[0]) {
				t.Fatalf("err = %v, want %s rejected", err, tc[0])
			}
		})
	}
}

func TestLoadObservability_defaults_localTextLoopbackMetricsNoSentry(t *testing.T) {
	// Arrange
	clearConfigEnv(t)

	// Act
	got, err := LoadObservability()

	// Assert
	if err != nil {
		t.Fatalf("LoadObservability: %v", err)
	}
	want := Observability{AppEnv: AppEnvLocal, LogFormat: LogFormatText, MetricsAddr: DefaultMetricsAddr}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadObservability_outsideLocal_defaultsToJSONLogs(t *testing.T) {
	for _, env := range []string{AppEnvStaging, AppEnvProduction} {
		t.Run(env, func(t *testing.T) {
			// Arrange
			clearConfigEnv(t)
			t.Setenv("APP_ENV", env)

			// Act
			got, err := LoadObservability()

			// Assert
			if err != nil {
				t.Fatalf("LoadObservability: %v", err)
			}
			if got.LogFormat != LogFormatJSON {
				t.Fatalf("LogFormat = %q, want json", got.LogFormat)
			}
		})
	}
}

func TestLoadObservability_rejectsUnknownValues(t *testing.T) {
	cases := map[string][2]string{
		"typo in env":    {"APP_ENV", "prod"},
		"unknown format": {"LOG_FORMAT", "yaml"},
		"addr no port":   {"METRICS_ADDR", "localhost"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			clearConfigEnv(t)
			t.Setenv(tc[0], tc[1])

			// Act
			_, err := LoadObservability()

			// Assert
			if err == nil || !strings.Contains(err.Error(), tc[0]) {
				t.Fatalf("err = %v, want %s rejected", err, tc[0])
			}
		})
	}
}

func TestLoadObservability_metricsOffLoopback_requiresToken(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9090", ":9090", "10.0.0.4:9090"} {
		t.Run(addr, func(t *testing.T) {
			// Arrange
			clearConfigEnv(t)
			t.Setenv("METRICS_ADDR", addr)

			// Act
			_, err := LoadObservability()

			// Assert
			if err == nil || !strings.Contains(err.Error(), "METRICS_TOKEN") {
				t.Fatalf("err = %v, want METRICS_TOKEN required so /metrics is never open to the network", err)
			}
		})
	}
}

func TestLoadObservability_metricsOffLoopback_withTokenIsAllowed(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	t.Setenv("METRICS_ADDR", "0.0.0.0:9090")
	t.Setenv("METRICS_TOKEN", "0123456789abcdef01234567")

	// Act
	got, err := LoadObservability()

	// Assert
	if err != nil {
		t.Fatalf("LoadObservability: %v", err)
	}
	if !got.MetricsEnabled() || got.MetricsToken == "" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadObservability_shortMetricsToken_isRejected(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	t.Setenv("METRICS_TOKEN", "hunter2")

	// Act
	_, err := LoadObservability()

	// Assert
	if err == nil || !strings.Contains(err.Error(), "METRICS_TOKEN") {
		t.Fatalf("err = %v, want short METRICS_TOKEN rejected", err)
	}
}

func TestLoadObservability_metricsOff_disablesListener(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	t.Setenv("METRICS_ADDR", "OFF")

	// Act
	got, err := LoadObservability()

	// Assert
	if err != nil {
		t.Fatalf("LoadObservability: %v", err)
	}
	if got.MetricsEnabled() {
		t.Fatal("MetricsEnabled = true with METRICS_ADDR=off")
	}
}
