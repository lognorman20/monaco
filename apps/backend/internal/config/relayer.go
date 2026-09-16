package config

import "fmt"

// Relayer holds the app fee payer key material registered at API startup.
// M1 validates presence only; transaction signing is deferred to M2 sweeps.
type Relayer struct {
	privateKey string
}

// PrivateKey returns the base58-encoded Solana keypair for the fee payer.
func (r *Relayer) PrivateKey() string {
	return r.privateKey
}

// LoadRelayer registers the relayer fee payer from config loaded via Load.
func LoadRelayer(cfg *Config) (*Relayer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.RelayerPrivateKey == "" {
		return nil, fmt.Errorf("%s is required", envRelayerPrivateKey)
	}
	return &Relayer{privateKey: cfg.RelayerPrivateKey}, nil
}
