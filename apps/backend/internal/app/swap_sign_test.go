package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/solana/key"
	"github.com/monaco/monaco/apps/backend/internal/solana/txsign"
)

func TestSwapService_signSwapTransaction_dualSign(t *testing.T) {
	relayerPriv, relayerPub := swapTestKeypair("swap-relayer")
	treasuryPriv, treasuryPub := swapTestKeypair("swap-treasury")
	relayerKey, err := key.ParsePrivateKey(swapEncodeBase58(relayerPriv))
	if err != nil {
		t.Fatalf("parse relayer key: %v", err)
	}

	unsigned := swapTestTransaction([][]byte{relayerPub, treasuryPub}, 2)
	unsignedB64 := base64.StdEncoding.EncodeToString(unsigned)

	svc := &SwapService{
		signer:     NewFakePrivyTreasurySigner(),
		relayerKey: relayerKey,
	}

	gotB64, err := svc.signSwapTransaction(context.Background(), "wallet-test", unsignedB64)
	if err != nil {
		t.Fatalf("sign swap transaction: %v", err)
	}
	if gotB64 == "" {
		t.Fatal("expected signed transaction")
	}

	afterRelayer, err := txsign.SignLocalSignerIfRequired(unsignedB64, relayerKey)
	if err != nil {
		t.Fatalf("sign relayer: %v", err)
	}
	afterRelayerBytes, err := base64.StdEncoding.DecodeString(afterRelayer)
	if err != nil {
		t.Fatalf("decode relayer signed: %v", err)
	}
	if ok, _ := txsign.HasSignature(afterRelayerBytes, 0); !ok {
		t.Fatal("expected relayer signature at index 0")
	}

	fullySigned, err := txsign.SignAtIndex(afterRelayerBytes, treasuryPriv, 1)
	if err != nil {
		t.Fatalf("sign treasury: %v", err)
	}
	allSigned, err := txsign.AllRequiredSignaturesPresent(fullySigned)
	if err != nil {
		t.Fatalf("check signatures: %v", err)
	}
	if !allSigned {
		t.Fatal("expected dual-signed transaction")
	}
}

func swapTestKeypair(label string) (ed25519.PrivateKey, ed25519.PublicKey) {
	seed := sha256.Sum256([]byte(label))
	priv := ed25519.NewKeyFromSeed(seed[:])
	return priv, priv.Public().(ed25519.PublicKey)
}

func swapTestTransaction(accounts [][]byte, signerCount int) []byte {
	header := []byte{byte(signerCount), 0, 0}
	blockhash := make([]byte, ed25519.PublicKeySize)

	var message []byte
	message = append(message, header...)
	message = append(message, byte(len(accounts)))
	for _, account := range accounts {
		message = append(message, account...)
	}
	message = append(message, blockhash...)
	message = append(message, 0)

	tx := make([]byte, 0, len(message)+signerCount*ed25519.SignatureSize+1)
	tx = append(tx, byte(signerCount))
	for i := 0; i < signerCount; i++ {
		tx = append(tx, make([]byte, ed25519.SignatureSize)...)
	}
	tx = append(tx, message...)
	return tx
}

const swapBase58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func swapEncodeBase58(input []byte) string {
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
		out[zeros+i-start] = swapBase58Alphabet[buf[i]]
	}
	return string(out)
}
