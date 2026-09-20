package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ParsePrivyVerificationKey parses the PEM public key Privy signs access tokens with.
// Env files often carry the PEM on one line with literal "\n"; both forms are accepted.
func ParsePrivyVerificationKey(raw string) (*ecdsa.PublicKey, error) {
	pem := strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))
	if pem == "" {
		return nil, fmt.Errorf("%s is required (Privy dashboard verification key, PEM): without it every authenticated route fails", envPrivyVerificationKey)
	}
	key, err := jwt.ParseECPublicKeyFromPEM([]byte(pem))
	if err != nil {
		return nil, fmt.Errorf("%s is not a PEM EC public key: %w", envPrivyVerificationKey, err)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%s must be a P-256 key: Privy signs access tokens with ES256", envPrivyVerificationKey)
	}
	return key, nil
}

// TestPrivyVerificationKeyPEM returns a valid P-256 public key for tests that only need
// config.Load to succeed. No private half exists, so nothing can be signed against it.
func TestPrivyVerificationKeyPEM() string {
	return `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEWJv6CehD5X6VBJZ59ElhGq8P9tBB
uQxbqIcH7yC5jasgAXRgi6y2Tx/jezLhJpnaan+MMTF5hk85oWMVyW5cyQ==
-----END PUBLIC KEY-----`
}
