// Package config loads Monaco API settings from environment variables.
package config

import (
	"crypto/ecdsa"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// SolanaCluster is the production Solana RPC cluster for chain operations.
const SolanaCluster = "mainnet-beta"

const (
	envDatabaseURL                  = "DATABASE_URL"
	envPrivyAppID                   = "PRIVY_APP_ID"
	envPrivyAppSecret               = "PRIVY_APP_SECRET"
	envPrivyAuthorizationPrivateKey = "PRIVY_AUTHORIZATION_PRIVATE_KEY"
	envPrivyAuthorizationKeyID      = "PRIVY_AUTHORIZATION_KEY_ID"
	envRelayerPrivateKey            = "RELAYER_PRIVATE_KEY"
	envSolanaRPCURL                 = "SOLANA_RPC_URL"
	envPythAPIKey                   = "PYTH_API_KEY"
	envPythHermesBaseURL            = "PYTH_HERMES_BASE_URL"
	envJupiterAPIKey                = "JUPITER_API_KEY"
	envSupabaseURL                  = "SUPABASE_URL"
	envSupabaseServiceRoleKey       = "SUPABASE_SERVICE_ROLE_KEY"
	envSwapProvider                 = "SWAP_PROVIDER"
	envFlashAPIKey                  = "FLASH_API_KEY"
	envFlashMaxSlippage             = "FLASH_MAX_SLIPPAGE"
	envPrivyVerificationKey         = "PRIVY_VERIFICATION_KEY"
)

// maxFlashSlippage caps FLASH_MAX_SLIPPAGE so a typo cannot open a treasury swap to a bad fill.
const maxFlashSlippage = 0.05

// Config holds runtime credentials for the Monaco API.
//
// Required environment variables:
//   - DATABASE_URL: Postgres connection string. Local dev uses compose:
//     postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable
//   - PRIVY_APP_ID: Privy application ID from the dashboard.
//   - PRIVY_APP_SECRET: Privy application secret from the dashboard.
//   - PRIVY_VERIFICATION_KEY: the app's ES256 verification key (PEM public key) from the Privy
//     dashboard. Every access token is checked against it, so it is parsed once here and a
//     missing or malformed key stops boot instead of failing each authenticated request.
//   - RELAYER_PRIVATE_KEY: Base58-encoded Solana secret key for the app fee payer (mainnet).
//     JSON [1,2,...] arrays from solana-keygen are auto-converted at startup; base58 is preferred.
//
// Optional environment variables:
//   - PRIVY_AUTHORIZATION_PRIVATE_KEY: Privy wallet authorization key (wallet-auth:… PKCS#8)
//     required for server-side wallet RPC such as signAndSendTransaction.
//   - PRIVY_AUTHORIZATION_KEY_ID: Privy authorization key quorum id (public config) added as
//     additional_signer on new member wallets so the server can sign sweeps. Existing wallets
//     created without this signer must be updated in Privy (owner-signed PATCH); new wallets
//     get the signer at create time when this is set.
//   - PYTH_API_KEY: Pyth Hermes API key (Bearer token) for marked equity price fetches (M4).
//     Equity feeds (e.g. AAPLx) require feed grants on the key in Pyth Terminal; crypto-only
//     keys authenticate but return 403 "Not entitled" for equity price updates.
//   - PYTH_HERMES_BASE_URL: Optional Hermes base URL override (default https://pyth.dourolabs.app/hermes).
//   - JUPITER_API_KEY: Jupiter Price API key (x-api-key header) for catalog/popular display
//     prices. Optional — the Price API also serves unauthenticated requests at a lower rate
//     limit — but set it in production to avoid 429s.
//   - SWAP_PROVIDER: venue for treasury buys and sells: "jupiter" (default) or "flash"
//     (Definitive Flash), including cash-out sells. Display quotes and routability probes stay on Jupiter.
//   - FLASH_API_KEY: Definitive Flash integrator key (x-definitive-api-key header). Required
//     when SWAP_PROVIDER=flash. Create one at app.definitive.fi > More > Flash.
//   - FLASH_MAX_SLIPPAGE: decimal slippage bound for Flash market orders (default 0.01 = 1%,
//     max 0.05).
//   - SOLANA_RPC_URL: Solana JSON-RPC endpoint for every chain read and confirmation. Unset
//     falls back to the public cluster endpoint, which has no SLA: set a paid RPC outside
//     local dev.
//   - DB_MAX_OPEN_CONNS, DB_MAX_IDLE_CONNS, DB_CONN_MAX_LIFETIME, DB_CONN_MAX_IDLE_TIME: see DBPool.
type Config struct {
	DatabaseURL                  string
	PrivyAppID                   string
	PrivyAppSecret               string
	PrivyAuthorizationPrivateKey string
	PrivyAuthorizationKeyID      string
	RelayerPrivateKey            string
	SolanaRPCURL                 string
	PythAPIKey                   string
	PythHermesBaseURL            string
	JupiterAPIKey                string
	SupabaseURL                  string
	SupabaseServiceRoleKey       string
	SolanaCluster                string
	SwapProvider                 string
	FlashAPIKey                  string
	FlashMaxSlippage             string
	// PrivyVerificationKey is the parsed PRIVY_VERIFICATION_KEY.
	PrivyVerificationKey *ecdsa.PublicKey
	DBPool               DBPool
}

// Load reads required settings from the process environment.
// Missing or blank values return an error naming the variable.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:                  strings.TrimSpace(os.Getenv(envDatabaseURL)),
		PrivyAppID:                   strings.TrimSpace(os.Getenv(envPrivyAppID)),
		PrivyAppSecret:               strings.TrimSpace(os.Getenv(envPrivyAppSecret)),
		PrivyAuthorizationPrivateKey: strings.TrimSpace(os.Getenv(envPrivyAuthorizationPrivateKey)),
		PrivyAuthorizationKeyID:      strings.TrimSpace(os.Getenv(envPrivyAuthorizationKeyID)),
		RelayerPrivateKey:            strings.TrimSpace(os.Getenv(envRelayerPrivateKey)),
		SolanaRPCURL:                 strings.TrimSpace(os.Getenv(envSolanaRPCURL)),
		PythAPIKey:                   strings.TrimSpace(os.Getenv(envPythAPIKey)),
		PythHermesBaseURL:            strings.TrimRight(strings.TrimSpace(os.Getenv(envPythHermesBaseURL)), "/"),
		JupiterAPIKey:                strings.TrimSpace(os.Getenv(envJupiterAPIKey)),
		SupabaseURL:                  strings.TrimSpace(os.Getenv(envSupabaseURL)),
		SupabaseServiceRoleKey:       strings.TrimSpace(os.Getenv(envSupabaseServiceRoleKey)),
		SolanaCluster:                SolanaCluster,
		FlashAPIKey:                  strings.TrimSpace(os.Getenv(envFlashAPIKey)),
		FlashMaxSlippage:             strings.TrimSpace(os.Getenv(envFlashMaxSlippage)),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("%s is required (local dev: postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable)", envDatabaseURL)
	}
	if cfg.PrivyAppID == "" {
		return nil, fmt.Errorf("%s is required", envPrivyAppID)
	}
	if cfg.PrivyAppSecret == "" {
		return nil, fmt.Errorf("%s is required", envPrivyAppSecret)
	}
	verificationKey, err := ParsePrivyVerificationKey(os.Getenv(envPrivyVerificationKey))
	if err != nil {
		return nil, err
	}
	cfg.PrivyVerificationKey = verificationKey
	if cfg.RelayerPrivateKey == "" {
		return nil, fmt.Errorf("%s is required", envRelayerPrivateKey)
	}
	normalizedRelayerKey, err := solanakey.ParsePrivateKey(cfg.RelayerPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envRelayerPrivateKey, err)
	}
	cfg.RelayerPrivateKey = normalizedRelayerKey
	if cfg.PrivyAuthorizationPrivateKey != "" && cfg.PrivyAuthorizationKeyID == "" {
		return nil, fmt.Errorf("%s is required when %s is set", envPrivyAuthorizationKeyID, envPrivyAuthorizationPrivateKey)
	}

	swapProvider, err := swapprovider.ParseName(strings.ToLower(strings.TrimSpace(os.Getenv(envSwapProvider))))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envSwapProvider, err)
	}
	cfg.SwapProvider = swapProvider
	if cfg.SwapProvider == swapprovider.NameFlash && cfg.FlashAPIKey == "" {
		return nil, fmt.Errorf("%s is required when %s=%s", envFlashAPIKey, envSwapProvider, swapprovider.NameFlash)
	}
	if cfg.FlashMaxSlippage != "" {
		slippage, err := strconv.ParseFloat(cfg.FlashMaxSlippage, 64)
		if err != nil || slippage <= 0 || slippage > maxFlashSlippage {
			return nil, fmt.Errorf("%s must be a decimal in (0, %g], got %q", envFlashMaxSlippage, maxFlashSlippage, cfg.FlashMaxSlippage)
		}
	}

	if err := validateSolanaRPCURL(cfg.SolanaRPCURL); err != nil {
		return nil, err
	}
	pool, err := loadDBPool()
	if err != nil {
		return nil, err
	}
	cfg.DBPool = pool

	return cfg, nil
}

// SolanaRPCEndpoint is the JSON-RPC endpoint every Solana client uses: SOLANA_RPC_URL when
// set, else the public endpoint of the cluster.
func (c *Config) SolanaRPCEndpoint() string {
	return SolanaRPCEndpoint(c.SolanaCluster, c.SolanaRPCURL)
}

// SolanaRPCEndpoint resolves an RPC endpoint from a cluster name and an optional override.
func SolanaRPCEndpoint(cluster, rpcURL string) string {
	if rpcURL = strings.TrimSpace(rpcURL); rpcURL != "" {
		return rpcURL
	}
	if cluster = strings.TrimSpace(cluster); cluster == "" {
		cluster = SolanaCluster
	}
	return fmt.Sprintf("https://api.%s.solana.com", cluster)
}

func validateSolanaRPCURL(raw string) error {
	if raw == "" {
		return nil
	}
	// The URL usually embeds an API key, so the error never echoes it.
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL", envSolanaRPCURL)
	}
	return nil
}
