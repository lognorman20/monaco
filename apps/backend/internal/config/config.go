// Package config loads Monaco API settings from environment variables.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	envDatabaseURL            = "DATABASE_URL"
	envDynamicEnvironmentID   = "DYNAMIC_ENVIRONMENT_ID"
	envDynamicAPIToken        = "DYNAMIC_API_TOKEN"
	envDynamicWalletPassword  = "DYNAMIC_WALLET_PASSWORD"
	envWalletSharesKey        = "WALLET_SHARES_KEY"
	envSignerURL              = "SIGNER_URL"
	envSignerSharedSecret     = "SIGNER_SHARED_SECRET"
	envBaseRPCURL             = "BASE_RPC_URL"
	envRelayerPrivateKey      = "RELAYER_PRIVATE_KEY"
	envKyberClientID          = "KYBER_CLIENT_ID"
	envPythAPIKey             = "PYTH_API_KEY"
	envPythHermesBaseURL      = "PYTH_HERMES_BASE_URL"
	envSupabaseURL            = "SUPABASE_URL"
	envSupabaseServiceRoleKey = "SUPABASE_SERVICE_ROLE_KEY"
)

const defaultSignerURL = "http://127.0.0.1:8081"
const defaultBaseRPCURL = "https://mainnet.base.org"
const defaultKyberClientID = "monaco"

// Config holds runtime credentials for the Monaco API.
type Config struct {
	DatabaseURL            string
	DynamicEnvironmentID   string
	DynamicAPIToken        string
	DynamicWalletPassword  string
	WalletSharesKey        string
	SignerURL              string
	SignerSharedSecret     string
	BaseRPCURL             string
	RelayerPrivateKey      string
	KyberClientID          string
	PythAPIKey             string
	PythHermesBaseURL      string
	SupabaseURL            string
	SupabaseServiceRoleKey string
}

// Load reads required settings from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:            strings.TrimSpace(os.Getenv(envDatabaseURL)),
		DynamicEnvironmentID:   strings.TrimSpace(os.Getenv(envDynamicEnvironmentID)),
		DynamicAPIToken:        strings.TrimSpace(os.Getenv(envDynamicAPIToken)),
		DynamicWalletPassword:  strings.TrimSpace(os.Getenv(envDynamicWalletPassword)),
		WalletSharesKey:        strings.TrimSpace(os.Getenv(envWalletSharesKey)),
		SignerURL:              strings.TrimSpace(os.Getenv(envSignerURL)),
		SignerSharedSecret:     strings.TrimSpace(os.Getenv(envSignerSharedSecret)),
		BaseRPCURL:             strings.TrimSpace(os.Getenv(envBaseRPCURL)),
		RelayerPrivateKey:      strings.TrimSpace(os.Getenv(envRelayerPrivateKey)),
		KyberClientID:          strings.TrimSpace(os.Getenv(envKyberClientID)),
		PythAPIKey:             strings.TrimSpace(os.Getenv(envPythAPIKey)),
		PythHermesBaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv(envPythHermesBaseURL)), "/"),
		SupabaseURL:            strings.TrimSpace(os.Getenv(envSupabaseURL)),
		SupabaseServiceRoleKey: strings.TrimSpace(os.Getenv(envSupabaseServiceRoleKey)),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("%s is required", envDatabaseURL)
	}
	if cfg.DynamicEnvironmentID == "" {
		return nil, fmt.Errorf("%s is required", envDynamicEnvironmentID)
	}
	if cfg.RelayerPrivateKey == "" {
		return nil, fmt.Errorf("%s is required", envRelayerPrivateKey)
	}
	if cfg.SignerSharedSecret == "" {
		return nil, fmt.Errorf("%s is required", envSignerSharedSecret)
	}
	if err := validateSharesKey(cfg.WalletSharesKey); err != nil {
		return nil, err
	}
	if cfg.SignerURL == "" {
		cfg.SignerURL = defaultSignerURL
	}
	if cfg.BaseRPCURL == "" {
		cfg.BaseRPCURL = defaultBaseRPCURL
	}
	if cfg.KyberClientID == "" {
		cfg.KyberClientID = defaultKyberClientID
	}
	if !strings.HasPrefix(cfg.RelayerPrivateKey, "0x") {
		return nil, fmt.Errorf("%s must be 0x-prefixed hex", envRelayerPrivateKey)
	}
	return cfg, nil
}

// RelayerAddress derives the relayer EOA from RELAYER_PRIVATE_KEY.
func (c *Config) RelayerAddress() (string, error) {
	if c == nil || c.RelayerPrivateKey == "" {
		return "", fmt.Errorf("relayer private key not configured")
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(c.RelayerPrivateKey, "0x"))
	if err != nil {
		return "", fmt.Errorf("relayer private key: %w", err)
	}
	return strings.ToLower(common.HexToAddress(crypto.PubkeyToAddress(key.PublicKey).Hex()).Hex()), nil
}

func validateSharesKey(raw string) error {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "0x")
	if raw == "" {
		return fmt.Errorf("%s is required", envWalletSharesKey)
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("%s must be 32-byte hex", envWalletSharesKey)
	}
	return nil
}
