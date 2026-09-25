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
	t.Setenv("PYTH_HERMES_BASE_URL", "")
	t.Setenv("JUPITER_API_KEY", "")
	t.Setenv("SWAP_PROVIDER", "")
	t.Setenv("FLASH_API_KEY", "")
	t.Setenv("FLASH_MAX_SLIPPAGE", "")
	t.Setenv("PRIVY_VERIFICATION_KEY", "")
	t.Setenv("SOLANA_RPC_URL", "")
	t.Setenv("TESSERA_API_BASE_URL", "")
	t.Setenv("TESSERA_ENABLED", "")
	for _, name := range []string{"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME", "DB_CONN_MAX_IDLE_TIME"} {
		t.Setenv(name, "")
	}
}

func setValidConfigEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable")
	t.Setenv("PRIVY_APP_ID", "test-privy-app-id")
	t.Setenv("PRIVY_APP_SECRET", "test-privy-app-secret")
	t.Setenv("RELAYER_PRIVATE_KEY", solanakey.TestPrivateKeyBase58())
	t.Setenv("PRIVY_AUTHORIZATION_PRIVATE_KEY", "wallet-auth:test-authorization-key")
	t.Setenv("PRIVY_AUTHORIZATION_KEY_ID", "test-authorization-key-id")
	t.Setenv("PRIVY_VERIFICATION_KEY", TestPrivyVerificationKeyPEM())
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

func TestLoad_optionalJupiterAPIKey_isLoadedWhenSet(t *testing.T) {
	// Arrange
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("JUPITER_API_KEY", "  test-jupiter-key  ")

	// Act
	cfg, err := Load()

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JupiterAPIKey != "test-jupiter-key" {
		t.Fatalf("JupiterAPIKey = %q", cfg.JupiterAPIKey)
	}
}

func TestLoad_optionalPythHermesBaseURL_isTrimmedWhenSet(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PYTH_HERMES_BASE_URL", " https://example.test/hermes/ ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PythHermesBaseURL != "https://example.test/hermes" {
		t.Fatalf("PythHermesBaseURL = %q", cfg.PythHermesBaseURL)
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
	t.Setenv("PRIVY_VERIFICATION_KEY", "  "+TestPrivyVerificationKeyPEM()+"  ")

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

func TestConfig_tesseraDefaults(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.TesseraEnabled {
		t.Fatal("TesseraEnabled = false, want true by default")
	}
	if cfg.TesseraAPIBaseURL != defaultTesseraAPIBaseURL {
		t.Fatalf("TesseraAPIBaseURL = %q, want %q", cfg.TesseraAPIBaseURL, defaultTesseraAPIBaseURL)
	}
}

func TestConfig_prestocksDefaults(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.PreStocksEnabled || cfg.PreStocksAPIBaseURL != defaultPreStocksAPIBaseURL {
		t.Fatalf("prestocks = enabled %v url %q", cfg.PreStocksEnabled, cfg.PreStocksAPIBaseURL)
	}
}

func TestConfig_prestocksDisabled(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("PRESTOCKS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PreStocksEnabled {
		t.Fatal("PreStocksEnabled = true, want false")
	}
}

func TestConfig_tesseraDisabled(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("TESSERA_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TesseraEnabled {
		t.Fatal("TesseraEnabled = true, want false")
	}
}

func TestLoad_swapProvider_defaultsToJupiter(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)

	cfg, err := Load()

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SwapProvider != "jupiter" {
		t.Fatalf("SwapProvider = %q, want jupiter", cfg.SwapProvider)
	}
}

func TestLoad_swapProviderFlash_requiresFlashAPIKey(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("SWAP_PROVIDER", "flash")

	_, err := Load()

	if err == nil || !strings.Contains(err.Error(), "FLASH_API_KEY") {
		t.Fatalf("Load error = %v, want FLASH_API_KEY required", err)
	}
}

func TestLoad_swapProviderFlash_loadsKeyAndSlippage(t *testing.T) {
	clearConfigEnv(t)
	setValidConfigEnv(t)
	t.Setenv("SWAP_PROVIDER", " Flash ")
	t.Setenv("FLASH_API_KEY", " test-flash-key ")
	t.Setenv("FLASH_MAX_SLIPPAGE", "0.02")

	cfg, err := Load()

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SwapProvider != "flash" || cfg.FlashAPIKey != "test-flash-key" || cfg.FlashMaxSlippage != "0.02" {
		t.Fatalf("unexpected flash config %+v", cfg)
	}
}

func TestLoad_swapProvider_unknownOrBadSlippage_returnsError(t *testing.T) {
	cases := map[string][2]string{
		"unknown provider":  {"SWAP_PROVIDER", "uniswap"},
		"slippage not num":  {"FLASH_MAX_SLIPPAGE", "1%"},
		"slippage too wide": {"FLASH_MAX_SLIPPAGE", "0.15"},
		"slippage zero":     {"FLASH_MAX_SLIPPAGE", "0"},
	}
	for name, env := range cases {
		clearConfigEnv(t)
		setValidConfigEnv(t)
		t.Setenv(env[0], env[1])

		if _, err := Load(); err == nil || !strings.Contains(err.Error(), env[0]) {
			t.Fatalf("%s: Load error = %v, want %s error", name, err, env[0])
		}
	}
}
