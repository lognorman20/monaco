package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const agentKeyPrefix = "mco_"

// MintAgentAPIKey returns a new plaintext key and its sha256 hash.
func MintAgentAPIKey() (plaintext, hash, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("mint agent api key: %w", err)
	}
	plaintext = agentKeyPrefix + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	if len(plaintext) >= 8 {
		prefix = plaintext[:8]
	}
	return plaintext, hash, prefix, nil
}

// HashAgentAPIKey hashes a presented agent API key for lookup.
func HashAgentAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
