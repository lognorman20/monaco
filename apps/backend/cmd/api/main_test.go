package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/config"
	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

func clearAPIEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PRIVY_APP_ID", "")
	t.Setenv("PRIVY_APP_SECRET", "")
	t.Setenv("RELAYER_PRIVATE_KEY", "")
	t.Setenv("PRIVY_VERIFICATION_KEY", "")
}

func setValidAPIEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	t.Setenv("PRIVY_APP_ID", "test-privy-app-id")
	t.Setenv("PRIVY_APP_SECRET", "test-privy-app-secret")
	t.Setenv("RELAYER_PRIVATE_KEY", solanakey.TestPrivateKeyBase58())
	t.Setenv("PRIVY_VERIFICATION_KEY", config.TestPrivyVerificationKeyPEM())
}

func TestAPIServer_missingRelayerKey_failsStartup(t *testing.T) {
	// Arrange
	clearAPIEnv(t)
	setValidAPIEnv(t)
	t.Setenv("RELAYER_PRIVATE_KEY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Act
	_, err := boot(ctx)

	// Assert
	if err == nil {
		t.Fatal("expected boot to fail without RELAYER_PRIVATE_KEY")
	}
	if !strings.Contains(err.Error(), "RELAYER_PRIVATE_KEY") {
		t.Fatalf("error = %q, want RELAYER_PRIVATE_KEY mentioned", err.Error())
	}
}
