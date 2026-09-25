package config

import (
	"os"
	"strings"
)

// Optional Solana treasury settings. Agent deployments (deploy_agent / recall_agent) send SPL
// USDC from a Privy Solana server wallet; without these the API boots and rejects those votes.
const (
	envPrivyAppID                   = "PRIVY_APP_ID"
	envPrivyAppSecret               = "PRIVY_APP_SECRET"
	envPrivyAuthorizationPrivateKey = "PRIVY_AUTHORIZATION_PRIVATE_KEY"
	envPrivyAuthorizationKeyID      = "PRIVY_AUTHORIZATION_KEY_ID"
	envSolanaRelayerPrivateKey      = "SOLANA_RELAYER_PRIVATE_KEY"
	envSolanaRPCURL                 = "SOLANA_RPC_URL"
	envClawpumpMCPURL               = "CLAWPUMP_MCP_URL"
)

// SolanaTreasuryConfig holds the optional Privy + Solana settings.
type SolanaTreasuryConfig struct {
	PrivyAppID                   string
	PrivyAppSecret               string
	PrivyAuthorizationPrivateKey string
	PrivyAuthorizationKeyID      string
	RelayerPrivateKey            string
	RPCURL                       string
	ClawpumpMCPURL               string
}

// LoadSolanaTreasury reads the optional Solana treasury settings from the environment.
func LoadSolanaTreasury() SolanaTreasuryConfig {
	env := func(key string) string { return strings.TrimSpace(os.Getenv(key)) }
	return SolanaTreasuryConfig{
		PrivyAppID:                   env(envPrivyAppID),
		PrivyAppSecret:               env(envPrivyAppSecret),
		PrivyAuthorizationPrivateKey: env(envPrivyAuthorizationPrivateKey),
		PrivyAuthorizationKeyID:      env(envPrivyAuthorizationKeyID),
		RelayerPrivateKey:            env(envSolanaRelayerPrivateKey),
		RPCURL:                       env(envSolanaRPCURL),
		ClawpumpMCPURL:               env(envClawpumpMCPURL),
	}
}

// Enabled reports whether Privy credentials and a Solana fee payer are all set.
func (c SolanaTreasuryConfig) Enabled() bool {
	return c.PrivyAppID != "" && c.PrivyAppSecret != "" && c.PrivyAuthorizationPrivateKey != "" && c.RelayerPrivateKey != ""
}
