// Package txsign adds local ed25519 signatures to partially signed Solana transactions.
package txsign

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

// SignLocalSignerIfRequired signs txBase64 when privateKeyBase58 matches a required
// signer slot that is still empty. Returns the original tx when the key is not a
// required signer (e.g. payer == taker with no gas sponsorship).
func SignLocalSignerIfRequired(txBase64, privateKeyBase58 string) (string, error) {
	if txBase64 == "" {
		return "", fmt.Errorf("transaction is required")
	}
	if privateKeyBase58 == "" {
		return txBase64, nil
	}

	priv, pub, err := decodeSolanaKeypair(privateKeyBase58)
	if err != nil {
		return "", fmt.Errorf("invalid local signer key: %w", err)
	}

	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", fmt.Errorf("decode transaction: %w", err)
	}

	index, err := RequiredSignerIndex(txBytes, pub)
	if err != nil {
		return "", err
	}
	if index < 0 {
		return txBase64, nil
	}
	if signed, err := HasSignature(txBytes, index); err != nil {
		return "", err
	} else if signed {
		return txBase64, nil
	}

	signed, err := SignAtIndex(txBytes, priv, index)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signed), nil
}

// RequiredSignerIndex returns the signature slot for pubkey among required signers,
// or -1 when pubkey is not a required signer.
func RequiredSignerIndex(txBytes, pubkey []byte) (int, error) {
	signers, err := RequiredSignerPubkeys(txBytes)
	if err != nil {
		return -1, err
	}
	for i, key := range signers {
		if bytes.Equal(key, pubkey) {
			return i, nil
		}
	}
	return -1, nil
}

// RequiredSignerPubkeys returns the static account keys that must sign the transaction.
func RequiredSignerPubkeys(txBytes []byte) ([][]byte, error) {
	message, err := TransactionMessage(txBytes)
	if err != nil {
		return nil, err
	}
	numRequired, accountKeys, err := parseMessageAccountKeys(message)
	if err != nil {
		return nil, err
	}
	if numRequired > len(accountKeys) {
		return nil, fmt.Errorf("transaction header requires %d signers but only %d account keys", numRequired, len(accountKeys))
	}
	return accountKeys[:numRequired], nil
}

// HasSignature reports whether signature slot index is populated.
func HasSignature(txBytes []byte, index int) (bool, error) {
	sigCount, sigOffset, err := decodeCompactU16(txBytes)
	if err != nil {
		return false, err
	}
	if index < 0 || index >= sigCount {
		return false, fmt.Errorf("signature index out of range")
	}
	start := sigOffset + index*ed25519.SignatureSize
	end := start + ed25519.SignatureSize
	if end > len(txBytes) {
		return false, fmt.Errorf("transaction missing signature block")
	}
	for _, b := range txBytes[start:end] {
		if b != 0 {
			return true, nil
		}
	}
	return false, nil
}

// AllRequiredSignaturesPresent reports whether every required signer slot is filled.
func AllRequiredSignaturesPresent(txBytes []byte) (bool, error) {
	sigCount, _, err := decodeCompactU16(txBytes)
	if err != nil {
		return false, err
	}
	for i := 0; i < sigCount; i++ {
		ok, err := HasSignature(txBytes, i)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// SignAtIndex signs message bytes at signature slot signerIndex.
func SignAtIndex(tx []byte, signer ed25519.PrivateKey, signerIndex int) ([]byte, error) {
	out := append([]byte(nil), tx...)
	sigCount, offset, err := decodeCompactU16(out)
	if err != nil {
		return nil, err
	}
	if signerIndex < 0 || signerIndex >= sigCount {
		return nil, fmt.Errorf("signer index out of range")
	}
	offset += sigCount * ed25519.SignatureSize
	if offset > len(out) {
		return nil, fmt.Errorf("transaction missing message")
	}
	message := out[offset:]

	signature := ed25519.Sign(signer, message)
	sigOffset, err := compactU16Size(sigCount)
	if err != nil {
		return nil, err
	}
	sigOffset += signerIndex * ed25519.SignatureSize
	if sigOffset+ed25519.SignatureSize > len(out) {
		return nil, fmt.Errorf("signer index out of range")
	}
	copy(out[sigOffset:sigOffset+ed25519.SignatureSize], signature)
	return out, nil
}

// TransactionMessage returns the serialized message bytes from a transaction.
func TransactionMessage(txBytes []byte) ([]byte, error) {
	sigCount, offset, err := decodeCompactU16(txBytes)
	if err != nil {
		return nil, err
	}
	offset += sigCount * ed25519.SignatureSize
	if offset > len(txBytes) {
		return nil, fmt.Errorf("transaction missing message")
	}
	return txBytes[offset:], nil
}

// SignatureAndBlockhash returns the base58 transaction id (the first signature) and the
// base58 recent blockhash of a fully signed transaction. The pair is what the chain needs
// to say whether the transaction landed or can no longer land.
func SignatureAndBlockhash(txBase64 string) (signature, blockhash string, err error) {
	txBytes, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		return "", "", fmt.Errorf("decode transaction: %w", err)
	}
	signed, err := AllRequiredSignaturesPresent(txBytes)
	if err != nil {
		return "", "", err
	}
	sigCount, sigOffset, err := decodeCompactU16(txBytes)
	if err != nil {
		return "", "", err
	}
	if sigCount == 0 || !signed {
		return "", "", fmt.Errorf("transaction is not fully signed")
	}

	message, err := TransactionMessage(txBytes)
	if err != nil {
		return "", "", err
	}
	_, accountKeys, err := parseMessageAccountKeys(message)
	if err != nil {
		return "", "", err
	}
	headerLen := 3
	if message[0] == 0x80 {
		headerLen = 4
	}
	countLen, err := compactU16Size(len(accountKeys))
	if err != nil {
		return "", "", err
	}
	hashStart := headerLen + countLen + len(accountKeys)*ed25519.PublicKeySize
	hashEnd := hashStart + 32
	if hashEnd > len(message) {
		return "", "", fmt.Errorf("transaction message truncated blockhash")
	}
	return solanakey.EncodeBase58(txBytes[sigOffset : sigOffset+ed25519.SignatureSize]),
		solanakey.EncodeBase58(message[hashStart:hashEnd]), nil
}

func parseMessageAccountKeys(message []byte) (numRequired int, accountKeys [][]byte, err error) {
	if len(message) < 3 {
		return 0, nil, fmt.Errorf("transaction message too short")
	}

	accountOffset := 0
	if message[0] == 0x80 {
		if len(message) < 4 {
			return 0, nil, fmt.Errorf("versioned transaction message too short")
		}
		numRequired = int(message[1])
		accountOffset = 4
	} else {
		numRequired = int(message[0])
		accountOffset = 3
	}

	accountCount, compactLen, err := decodeCompactU16(message[accountOffset:])
	if err != nil {
		return 0, nil, err
	}
	keysStart := accountOffset + compactLen
	keysEnd := keysStart + accountCount*ed25519.PublicKeySize
	if keysEnd > len(message) {
		return 0, nil, fmt.Errorf("transaction message truncated account keys")
	}

	accountKeys = make([][]byte, accountCount)
	for i := 0; i < accountCount; i++ {
		start := keysStart + i*ed25519.PublicKeySize
		key := make([]byte, ed25519.PublicKeySize)
		copy(key, message[start:start+ed25519.PublicKeySize])
		accountKeys[i] = key
	}
	return numRequired, accountKeys, nil
}

func decodeSolanaKeypair(encoded string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	normalized, err := solanakey.ParsePrivateKey(encoded)
	if err != nil {
		return nil, nil, err
	}
	raw, err := decodeBase58(normalized)
	if err != nil {
		return nil, nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		priv := ed25519.NewKeyFromSeed(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	default:
		return nil, nil, fmt.Errorf("unexpected key length %d", len(raw))
	}
}

func compactU16Size(value int) (int, error) {
	encoded := encodeCompactU16(value)
	return len(encoded), nil
}

func decodeCompactU16(data []byte) (int, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("empty compact-u16")
	}
	size := int(data[0])
	if size < 0x80 {
		return size, 1, nil
	}
	if len(data) < 2 {
		return 0, 0, fmt.Errorf("truncated compact-u16")
	}
	size = int(data[0]&0x7f) | int(data[1])<<7
	return size, 2, nil
}

func encodeCompactU16(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	return []byte{byte(n&0x7f | 0x80), byte(n >> 7)}
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

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
