package config

import (
	"strings"
	"testing"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

func clearConfigEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PRIVY_APP_ID", "")
	t.Setenv("PRIVY_APP_SECRET", "")
	t.Setenv("PRIVY_AUTHORIZATION_PRIVATE_KEY", "")
	t.Setenv("PRIVY_AUTHORIZATION_KEY_ID", "")
	t.Setenv("RELAYER_PRIVATE_KEY", "")
	t.Setenv("PYTH_API_KEY", "")
}

func setValidConfigEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	t.Setenv("PRIVY_APP_ID", "test-privy-app-id")
	t.Setenv("PRIVY_APP_SECRET", "test-privy-app-secret")
	t.Setenv("RELAYER_PRIVATE_KEY", solanakey.TestPrivateKeyBase58())
	t.Setenv("PRIVY_AUTHORIZATION_PRIVATE_KEY", "wallet-auth:test-authorization-key")
	t.Setenv("PRIVY_AUTHORIZATION_KEY_ID", "test-authorization-key-id")
}

func TestLoad_returnsConfigWhenAllRequiredEnvVarsSet(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.PrivyAppID != "test-privy-app-id" {
		t.Fatalf("PrivyAppID = %q", cfg.PrivyAppID)
	}
	if cfg.PrivyAppSecret != "test-privy-app-secret" {
		t.Fatalf("PrivyAppSecret = %q", cfg.PrivyAppSecret)
	}
	if cfg.RelayerPrivateKey != solanakey.TestPrivateKeyBase58() {
		t.Fatalf("RelayerPrivateKey = %q", cfg.RelayerPrivateKey)
	}
	if cfg.SolanaCluster != SolanaCluster {
		t.Fatalf("SolanaCluster = %q, want %q", cfg.SolanaCluster, SolanaCluster)
	}
	if cfg.PrivyAuthorizationPrivateKey != "wallet-auth:test-authorization-key" {
		t.Fatalf("PrivyAuthorizationPrivateKey = %q", cfg.PrivyAuthorizationPrivateKey)
	}
	if cfg.PrivyAuthorizationKeyID != "test-authorization-key-id" {
		t.Fatalf("PrivyAuthorizationKeyID = %q", cfg.PrivyAuthorizationKeyID)
	}
}

func TestLoad_missingDatabaseURL_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("DATABASE_URL", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for missing DATABASE_URL")
	}
	if err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestLoad_missingPrivyAppID_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRIVY_APP_ID", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for missing PRIVY_APP_ID")
	}
}

func TestLoad_missingPrivyAppSecret_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRIVY_APP_SECRET", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for missing PRIVY_APP_SECRET")
	}
}

func TestLoad_missingAuthorizationKeyIDWhenPrivateKeySet_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRIVY_AUTHORIZATION_KEY_ID", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for missing PRIVY_AUTHORIZATION_KEY_ID when private key is set")
	}
}

func TestLoad_missingRelayerPrivateKey_returnsError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("RELAYER_PRIVATE_KEY", "")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for missing RELAYER_PRIVATE_KEY")
	}
}

func TestLoad_optionalPythAPIKey_isLoadedWhenSet(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PYTH_API_KEY", "  test-pyth-key  ")

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PythAPIKey != "test-pyth-key" {
		t.Fatalf("PythAPIKey = %q", cfg.PythAPIKey)
	}
}

func TestLoad_jsonRelayerPrivateKey_isNormalizedToBase58(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	want := solanakey.TestPrivateKeyBase58()
	t.Setenv("RELAYER_PRIVATE_KEY", solanakey.TestPrivateKeyJSONIntArray())

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RelayerPrivateKey != want {
		t.Fatalf("RelayerPrivateKey = %q, want normalized %q", cfg.RelayerPrivateKey, want)
	}
}

func TestLoad_invalidRelayerPrivateKeyFormat_returnsHelpfulError(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("RELAYER_PRIVATE_KEY", "[1,2,not-an-int]")

	// Act
	_, err := Load()

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid relayer key")
	}
	if !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("error = %q, want JSON array guidance", err.Error())
	}
}

func TestLoad_trimsWhitespaceFromEnvValues(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	t.Setenv("DATABASE_URL", "  postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable  ")
	t.Setenv("PRIVY_APP_ID", "  app-id  ")
	t.Setenv("PRIVY_APP_SECRET", "  app-secret  ")
	t.Setenv("RELAYER_PRIVATE_KEY", "  "+solanakey.TestPrivateKeyBase58()+"  ")

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PrivyAppID != "app-id" {
		t.Fatalf("PrivyAppID = %q", cfg.PrivyAppID)
	}
}
