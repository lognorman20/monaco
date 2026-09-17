package config

import (
	"fmt"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

// Relayer holds the app fee payer key material registered at API startup.
// M1 validates presence only; transaction signing is deferred to M2 sweeps.
type Relayer struct {
	privateKey string
	publicKey  string
}

// PrivateKey returns the base58-encoded Solana keypair for the fee payer.
func (r *Relayer) PrivateKey() string {
	return r.privateKey
}

// PublicKey returns the base58-encoded Solana public key for the fee payer.
func (r *Relayer) PublicKey() string {
	if r == nil {
		return ""
	}
	return r.publicKey
}

// LoadRelayer registers the relayer fee payer from config loaded via Load.
func LoadRelayer(cfg *Config) (*Relayer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.RelayerPrivateKey == "" {
		return nil, fmt.Errorf("%s is required", envRelayerPrivateKey)
	}
	publicKey, err := solanakey.PublicKeyBase58FromPrivateKey(cfg.RelayerPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%s public key: %w", envRelayerPrivateKey, err)
	}
	return &Relayer{
		privateKey: cfg.RelayerPrivateKey,
		publicKey:  publicKey,
	}, nil
}
