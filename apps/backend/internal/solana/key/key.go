// Package key parses Solana private keys from common environment formats.
package key

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// ParsePrivateKey normalizes RELAYER_PRIVATE_KEY-style values to base58 and validates length.
//
// Accepted inputs:
//   - base58-encoded 32-byte seed or 64-byte secret key
//   - JSON byte arrays exported by solana-keygen, e.g. [1,2,...]
//   - values wrapped in single or double quotes from dotenv
func ParsePrivateKey(raw string) (string, error) {
	normalized, err := normalizePrivateKeyInput(raw)
	if err != nil {
		return "", err
	}

	decoded, err := decodeBase58(normalized)
	if err != nil {
		if strings.ContainsRune(raw, '[') || strings.ContainsRune(raw, ']') {
			return "", fmt.Errorf("invalid Solana private key: %v (RELAYER_PRIVATE_KEY must be base58, not a JSON [1,2,...] array; re-export from solana-keygen or Phantom as base58)", err)
		}
		return "", fmt.Errorf("invalid Solana private key: %v (RELAYER_PRIVATE_KEY must be a base58-encoded 32- or 64-byte secret key)", err)
	}

	switch len(decoded) {
	case ed25519.SeedSize, ed25519.PrivateKeySize:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid Solana private key length %d (expected 32-byte seed or 64-byte secret key; use base58, not JSON array)", len(decoded))
	}
}

func normalizePrivateKeyInput(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty value")
	}
	raw = trimEnvQuotes(raw)
	if raw == "" {
		return "", fmt.Errorf("empty value after trimming quotes")
	}

	if strings.HasPrefix(raw, "[") {
		bytes, err := parseJSONKeyBytes(raw)
		if err != nil {
			return "", fmt.Errorf("RELAYER_PRIVATE_KEY looks like JSON array but failed to parse: %v (use base58 secret key string instead of [1,2,...])", err)
		}
		return encodeBase58(bytes), nil
	}

	if strings.ContainsRune(raw, '[') || strings.ContainsRune(raw, ']') {
		return "", fmt.Errorf("RELAYER_PRIVATE_KEY contains brackets; set a base58-encoded Solana secret key, not a JSON byte array")
	}

	return raw, nil
}

func trimEnvQuotes(raw string) string {
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func parseJSONKeyBytes(raw string) ([]byte, error) {
	var bytes []byte
	if err := json.Unmarshal([]byte(raw), &bytes); err == nil {
		return bytes, nil
	}

	var ints []int
	if err := json.Unmarshal([]byte(raw), &ints); err != nil {
		return nil, err
	}
	out := make([]byte, len(ints))
	for i, v := range ints {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("byte at index %d out of range: %d", i, v)
		}
		out[i] = byte(v)
	}
	return out, nil
}

func decodeBase58(input string) ([]byte, error) {
	zeros := 0
	for zeros < len(input) && input[zeros] == '1' {
		zeros++
	}

	size := (len(input)*733/1000) + 1
	buf := make([]byte, size)
	for _, r := range input {
		val := int8(-1)
		for i := 0; i < len(base58Alphabet); i++ {
			if base58Alphabet[i] == byte(r) {
				val = int8(i)
				break
			}
		}
		if val < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", r)
		}

		carry := int(val)
		for i := len(buf) - 1; i >= 0; i-- {
			carry += 58 * int(buf[i])
			buf[i] = byte(carry & 0xff)
			carry >>= 8
		}
		if carry != 0 {
			return nil, fmt.Errorf("base58 overflow")
		}
	}

	start := 0
	for start < len(buf) && buf[start] == 0 {
		start++
	}

	out := make([]byte, zeros+len(buf)-start)
	copy(out[zeros:], buf[start:])
	return out, nil
}

// PublicKeyBase58FromPrivateKey derives the base58 Solana public key from a private key string.
func PublicKeyBase58FromPrivateKey(raw string) (string, error) {
	normalized, err := ParsePrivateKey(raw)
	if err != nil {
		return "", err
	}
	decoded, err := decodeBase58(normalized)
	if err != nil {
		return "", err
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		return encodeBase58(ed25519.NewKeyFromSeed(decoded).Public().(ed25519.PublicKey)), nil
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(decoded)
		return encodeBase58(priv.Public().(ed25519.PublicKey)), nil
	default:
		return "", fmt.Errorf("invalid Solana private key length %d", len(decoded))
	}
}

// TestPrivateKeyBase58 returns a deterministic valid base58 private key for unit tests.
func TestPrivateKeyBase58() string {
	seed := sha256.Sum256([]byte("monaco-test-relayer-seed"))
	priv := ed25519.NewKeyFromSeed(seed[:])
	parsed, err := ParsePrivateKey(encodeBase58(priv))
	if err != nil {
		panic(err)
	}
	return parsed
}

// TestPrivateKeyJSONIntArray returns the test relayer key as a solana-keygen-style int array.
func TestPrivateKeyJSONIntArray() string {
	seed := sha256.Sum256([]byte("monaco-test-relayer-seed"))
	priv := ed25519.NewKeyFromSeed(seed[:])
	ints := make([]int, len(priv))
	for i, b := range priv {
		ints[i] = int(b)
	}
	raw, err := json.Marshal(ints)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func encodeBase58(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}

	size := len(input)*138/100 + 1
	buf := make([]byte, size)
	start := size
	for _, b := range input {
		carry := int(b)
		for i := size - 1; i >= start; i-- {
			carry += 256 * int(buf[i])
			buf[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			start--
			buf[start] = byte(carry % 58)
			carry /= 58
		}
	}

	for i := start; i < size && buf[i] == 0; i++ {
		zeros++
	}

	out := make([]byte, zeros+size-start)
	for i := 0; i < zeros; i++ {
		out[i] = '1'
	}
	for i := start; i < size; i++ {
		out[zeros+i-start] = base58Alphabet[buf[i]]
	}
	return string(out)
}

// EncodeBase58 encodes bytes with the Solana (Bitcoin) base58 alphabet.
func EncodeBase58(input []byte) string {
	return encodeBase58(input)
}

// DecodeBase58 decodes a Solana (Bitcoin alphabet) base58 string.
func DecodeBase58(input string) ([]byte, error) {
	return decodeBase58(input)
}
