package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

const testRelayerKey = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efc684173c51d714e00"

func clearAPIEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DYNAMIC_ENVIRONMENT_ID", "")
	t.Setenv("RELAYER_PRIVATE_KEY", "")
}

func setValidAPIEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	t.Setenv("DYNAMIC_ENVIRONMENT_ID", "test-dynamic-env")
	t.Setenv("RELAYER_PRIVATE_KEY", testRelayerKey)
	t.Setenv("SIGNER_SHARED_SECRET", "test-signer-secret")
}

func TestAPIServer_missingRelayerKey_failsStartup(t *testing.T) {
	clearAPIEnv(t)
	setValidAPIEnv(t)
	t.Setenv("RELAYER_PRIVATE_KEY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := boot(ctx)

	if err == nil {
		t.Fatal("expected boot to fail without RELAYER_PRIVATE_KEY")
	}
	if !strings.Contains(err.Error(), "RELAYER_PRIVATE_KEY") {
		t.Fatalf("error = %q, want RELAYER_PRIVATE_KEY mentioned", err.Error())
	}
}
