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
	envDatabaseURL        = "DATABASE_URL"
	envPrivyAppID         = "PRIVY_APP_ID"
	envPrivyAppSecret     = "PRIVY_APP_SECRET"
	envRelayerPrivateKey  = "RELAYER_PRIVATE_KEY"
)

// Config holds runtime credentials for the Monaco API.
//
// Required environment variables:
//   - DATABASE_URL: Postgres connection string. Local dev uses compose:
//     postgres://monaco:monaco@localhost:54322/monaco?sslmode=disable
//   - PRIVY_APP_ID: Privy application ID from the dashboard.
//   - PRIVY_APP_SECRET: Privy application secret from the dashboard.
//   - RELAYER_PRIVATE_KEY: Base58-encoded Solana keypair for the app fee payer (mainnet).
type Config struct {
	DatabaseURL        string
	PrivyAppID         string
	PrivyAppSecret     string
	RelayerPrivateKey  string
	SolanaCluster      string
}

// Load reads required settings from the process environment.
// Missing or blank values return an error naming the variable.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:       strings.TrimSpace(os.Getenv(envDatabaseURL)),
		PrivyAppID:        strings.TrimSpace(os.Getenv(envPrivyAppID)),
		PrivyAppSecret:    strings.TrimSpace(os.Getenv(envPrivyAppSecret)),
		RelayerPrivateKey: strings.TrimSpace(os.Getenv(envRelayerPrivateKey)),
		SolanaCluster:     SolanaCluster,
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

	return cfg, nil
}
