package config

import (
	"testing"
)

const testRelayerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efc684173c51d714e00"

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"DATABASE_URL", "DYNAMIC_ENVIRONMENT_ID", "DYNAMIC_API_TOKEN", "DYNAMIC_WALLET_PASSWORD",
		"WALLET_SHARES_KEY", "SIGNER_URL", "SIGNER_SHARED_SECRET", "BASE_RPC_URL", "RELAYER_PRIVATE_KEY",
		"KYBER_CLIENT_ID", "PYTH_API_KEY", "PYTH_HERMES_BASE_URL",
	} {
		t.Setenv(k, "")
	}
}

func setValidConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	t.Setenv("DYNAMIC_ENVIRONMENT_ID", "test-dynamic-env")
	t.Setenv("RELAYER_PRIVATE_KEY", testRelayerKey)
	t.Setenv("SIGNER_SHARED_SECRET", "test-signer-secret")
	t.Setenv("WALLET_SHARES_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
}

func TestLoad_returnsConfigWhenAllRequiredEnvVarsSet(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DynamicEnvironmentID != "test-dynamic-env" {
		t.Fatalf("DynamicEnvironmentID = %q", cfg.DynamicEnvironmentID)
	}
	if cfg.SignerURL != defaultSignerURL {
		t.Fatalf("SignerURL = %q", cfg.SignerURL)
	}
	if cfg.BaseRPCURL != defaultBaseRPCURL {
		t.Fatalf("BaseRPCURL = %q", cfg.BaseRPCURL)
	}
	if cfg.KyberClientID != defaultKyberClientID {
		t.Fatalf("KyberClientID = %q", cfg.KyberClientID)
	}
	if _, err := cfg.RelayerAddress(); err != nil {
		t.Fatalf("RelayerAddress: %v", err)
	}
}

func TestLoad_missingDatabaseURL_returnsError(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_missingDynamicEnvironmentID_returnsError(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("DYNAMIC_ENVIRONMENT_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}
