package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

const (
	// AgentKeyPrefix marks a Monaco agent key so secret scanners and humans recognise one.
	AgentKeyPrefix = "monaco_ak_"
	// agentKeySecretLength characters of a 31 symbol alphabet carry ~158 bits, past the 128
	// a key that moves treasury funds without a vote needs. Guessing is not a realistic attack.
	agentKeySecretLength = 32
	agentKeyAlphabet     = "23456789abcdefghjkmnpqrstuvwxyz" // no 0/O, 1/l/I
	// agentKeyDisplayLength is how much of the secret api_key_prefix keeps for telling keys apart.
	agentKeyDisplayLength = 4
)

// MintAgentAPIKey returns a new plaintext key, its sha256 hash, and a short non-secret prefix.
func MintAgentAPIKey() (plaintext, hash, prefix string, err error) {
	alphabetLen := big.NewInt(int64(len(agentKeyAlphabet)))
	buf := make([]byte, agentKeySecretLength)
	for i := range buf {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", "", "", fmt.Errorf("mint agent api key: %w", err)
		}
		buf[i] = agentKeyAlphabet[n.Int64()]
	}
	plaintext = AgentKeyPrefix + string(buf)
	hash = HashAgentAPIKey(plaintext)
	prefix = plaintext[:len(AgentKeyPrefix)+agentKeyDisplayLength]
	return plaintext, hash, prefix, nil
}

// HashAgentAPIKey hashes a presented agent API key for lookup.
func HashAgentAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// IsCurrentAgentKeyFormat reports whether key has the shape MintAgentAPIKey produces. Keys
// minted before it (five characters, no prefix) still authenticate; they are just guessable,
// so the wrong-key throttle treats them more strictly.
func IsCurrentAgentKeyFormat(key string) bool {
	secret, ok := strings.CutPrefix(key, AgentKeyPrefix)
	if !ok || len(secret) != agentKeySecretLength {
		return false
	}
	for _, c := range secret {
		if !strings.ContainsRune(agentKeyAlphabet, c) {
			return false
		}
	}
	return true
}
