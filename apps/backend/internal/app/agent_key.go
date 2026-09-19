package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

const (
	agentKeyLength   = 5
	agentKeyAlphabet = "23456789abcdefghjkmnpqrstuvwxyz" // no 0/O, 1/l/I
)

// MintAgentAPIKey returns a new plaintext key and its sha256 hash.
func MintAgentAPIKey() (plaintext, hash, prefix string, err error) {
	alphabetLen := big.NewInt(int64(len(agentKeyAlphabet)))
	buf := make([]byte, agentKeyLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", "", "", fmt.Errorf("mint agent api key: %w", err)
		}
		buf[i] = agentKeyAlphabet[n.Int64()]
	}
	plaintext = string(buf)
	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	prefix = plaintext
	return plaintext, hash, prefix, nil
}

// HashAgentAPIKey hashes a presented agent API key for lookup.
func HashAgentAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
