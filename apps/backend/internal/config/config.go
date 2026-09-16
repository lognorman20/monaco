// Package config loads Monaco API settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
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
)

// Config holds runtime credentials for the Monaco API.
//
// Required environment variables:
//   - DATABASE_URL: Postgres connection string. Local dev uses compose:
//     postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable
//   - PRIVY_APP_ID: Privy application ID from the dashboard.
//   - PRIVY_APP_SECRET: Privy application secret from the dashboard.
//   - RELAYER_PRIVATE_KEY: Base58-encoded Solana keypair for the app fee payer (mainnet).
//
// Optional environment variables:
//   - PRIVY_AUTHORIZATION_PRIVATE_KEY: Privy wallet authorization key (wallet-auth:… PKCS#8)
//     required for server-side wallet RPC such as signAndSendTransaction.
//   - PRIVY_AUTHORIZATION_KEY_ID: Privy authorization key quorum id (public config) added as
//     additional_signer on new member wallets so the server can sign sweeps. Existing wallets
//     created without this signer must be updated in Privy (owner-signed PATCH); new wallets
//     get the signer at create time when this is set.
type Config struct {
	DatabaseURL                   string
	PrivyAppID                    string
	PrivyAppSecret                string
	PrivyAuthorizationPrivateKey  string
	PrivyAuthorizationKeyID       string
	RelayerPrivateKey             string
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
	if cfg.PrivyAuthorizationPrivateKey != "" && cfg.PrivyAuthorizationKeyID == "" {
		return nil, fmt.Errorf("%s is required when %s is set", envPrivyAuthorizationKeyID, envPrivyAuthorizationPrivateKey)
	}

	return cfg, nil
}
