package config

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

// Relayer holds the app relayer key material registered at API startup.
type Relayer struct {
	privateKey string
	address    string
}

// PrivateKey returns the 0x-prefixed secp256k1 private key hex.
func (r *Relayer) PrivateKey() string {
	return r.privateKey
}

// Address returns the lowercase relayer EOA.
func (r *Relayer) Address() string {
	if r == nil {
		return ""
	}
	return r.address
}

// PublicKey is kept for legacy call sites that logged a relayer pubkey at boot.
func (r *Relayer) PublicKey() string {
	return r.Address()
}

// LoadRelayer registers the relayer from config loaded via Load.
func LoadRelayer(cfg *Config) (*Relayer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	addr, err := cfg.RelayerAddress()
	if err != nil {
		return nil, err
	}
	return &Relayer{privateKey: cfg.RelayerPrivateKey, address: addr}, nil
}

// MustRelayerAddress derives the address or panics (tests).
func MustRelayerAddress(cfg *Config) string {
	addr, err := cfg.RelayerAddress()
	if err != nil {
		panic(err)
	}
	_ = crypto.Keccak256Hash([]byte(addr))
	return addr
}
