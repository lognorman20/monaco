// Package config loads Monaco API settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

// SolanaCluster is the production Solana RPC cluster for chain operations.
const SolanaCluster = "mainnet-beta"

const (
	envDatabaseURL                   = "DATABASE_URL"
	envPrivyAppID                    = "PRIVY_APP_ID"
	envPrivyAppSecret                = "PRIVY_APP_SECRET"
	envPrivyAuthorizationPrivateKey  = "PRIVY_AUTHORIZATION_PRIVATE_KEY"
	envPrivyAuthorizationKeyID       = "PRIVY_AUTHORIZATION_KEY_ID"
	envRelayerPrivateKey             = "RELAYER_PRIVATE_KEY"
	envSolanaRPCURL                  = "SOLANA_RPC_URL"
	envPythAPIKey                    = "PYTH_API_KEY"
	envPythHermesBaseURL             = "PYTH_HERMES_BASE_URL"
	envSupabaseURL                   = "SUPABASE_URL"
	envSupabaseServiceRoleKey        = "SUPABASE_SERVICE_ROLE_KEY"
)

// Config holds runtime credentials for the Monaco API.
//
// Required environment variables:
//   - DATABASE_URL: Postgres connection string. Local dev uses compose:
//     postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable
//   - PRIVY_APP_ID: Privy application ID from the dashboard.
//   - PRIVY_APP_SECRET: Privy application secret from the dashboard.
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
type Config struct {
	DatabaseURL                   string
	PrivyAppID                    string
	PrivyAppSecret                string
	PrivyAuthorizationPrivateKey  string
	PrivyAuthorizationKeyID       string
	RelayerPrivateKey             string
	SolanaRPCURL                  string
	PythAPIKey                    string
	PythHermesBaseURL             string
	SupabaseURL                   string
	SupabaseServiceRoleKey        string
	SolanaCluster                 string
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
		SupabaseURL:                  strings.TrimSpace(os.Getenv(envSupabaseURL)),
		SupabaseServiceRoleKey:       strings.TrimSpace(os.Getenv(envSupabaseServiceRoleKey)),
		SolanaCluster:                SolanaCluster,
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

	return cfg, nil
}
