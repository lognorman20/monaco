package txsign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

func TestSignLocalSignerIfRequired_dualSignRelayerThenTreasury(t *testing.T) {
	relayerPriv, relayerPub := testKeypair("relayer-wallet")
	treasuryPriv, treasuryPub := testKeypair("treasury-wallet")

	unsigned := encodeTestTransaction([][]byte{relayerPub, treasuryPub}, [][]byte{relayerPriv, treasuryPriv})
	unsignedB64 := base64.StdEncoding.EncodeToString(unsigned)

	relayerKey, err := solanakey.ParsePrivateKey(encodeBase58(relayerPriv))
	if err != nil {
		t.Fatalf("parse relayer key: %v", err)
	}

	afterRelayer, err := SignLocalSignerIfRequired(unsignedB64, relayerKey)
	if err != nil {
		t.Fatalf("sign relayer: %v", err)
	}
	relayerSigned, err := base64.StdEncoding.DecodeString(afterRelayer)
	if err != nil {
		t.Fatalf("decode relayer signed tx: %v", err)
	}
	if ok, _ := HasSignature(relayerSigned, 0); !ok {
		t.Fatal("expected relayer signature at index 0")
	}
	if ok, _ := HasSignature(relayerSigned, 1); ok {
		t.Fatal("treasury signature should still be empty after relayer sign")
	}

	afterTreasury, err := SignAtIndex(relayerSigned, treasuryPriv, 1)
	if err != nil {
		t.Fatalf("sign treasury: %v", err)
	}
	allSigned, err := AllRequiredSignaturesPresent(afterTreasury)
	if err != nil {
		t.Fatalf("check signatures: %v", err)
	}
	if !allSigned {
		t.Fatal("expected all required signatures present")
	}
}

func TestSignLocalSignerIfRequired_skipsWhenNotRequiredSigner(t *testing.T) {
	treasuryPriv, treasuryPub := testKeypair("treasury-only")
	unsigned := encodeTestTransaction([][]byte{treasuryPub}, [][]byte{treasuryPriv})
	unsignedB64 := base64.StdEncoding.EncodeToString(unsigned)

	relayerKey := solanakey.TestPrivateKeyBase58()
	got, err := SignLocalSignerIfRequired(unsignedB64, relayerKey)
	if err != nil {
		t.Fatalf("sign local signer: %v", err)
	}
	if got != unsignedB64 {
		t.Fatal("expected unchanged transaction when local signer is not required")
	}
}

func TestRequiredSignerIndex_versionedMessage(t *testing.T) {
	_, relayerPub := testKeypair("relayer-v0")
	treasuryPriv, treasuryPub := testKeypair("treasury-v0")

	message := encodeVersionedMessage([][]byte{relayerPub, treasuryPub})
	unsigned := encodeTestTransactionFromMessage(message, 2)

	index, err := RequiredSignerIndex(unsigned, relayerPub)
	if err != nil {
		t.Fatalf("required signer index: %v", err)
	}
	if index != 0 {
		t.Fatalf("relayer index = %d, want 0", index)
	}

	signed, err := SignAtIndex(unsigned, treasuryPriv, 1)
	if err != nil {
		t.Fatalf("sign treasury: %v", err)
	}
	if ok, _ := HasSignature(signed, 1); !ok {
		t.Fatal("expected treasury signature at index 1")
	}
}

func encodeTestTransaction(signers [][]byte, privateKeys [][]byte) []byte {
	message := encodeLegacyMessage(signers)
	return encodeTestTransactionFromMessage(message, len(signers))
}

func encodeTestTransactionFromMessage(message []byte, signerCount int) []byte {
	tx := make([]byte, 0, len(message)+signerCount*ed25519.SignatureSize+8)
	tx = append(tx, encodeCompactU16(signerCount)...)
	for i := 0; i < signerCount; i++ {
		tx = append(tx, make([]byte, ed25519.SignatureSize)...)
	}
	tx = append(tx, message...)
	return tx
}

func encodeLegacyMessage(accounts [][]byte) []byte {
	header := []byte{byte(len(accounts)), 0, 0}
	blockhash := make([]byte, ed25519.PublicKeySize)
	copy(blockhash, []byte("recent-blockhash-placeholder!!"))

	var out []byte
	out = append(out, header...)
	out = append(out, encodeCompactU16(len(accounts))...)
	for _, account := range accounts {
		out = append(out, account...)
	}
	out = append(out, blockhash...)
	out = append(out, encodeCompactU16(0)...)
	return out
}

func encodeVersionedMessage(accounts [][]byte) []byte {
	header := []byte{0x80, byte(len(accounts)), 0, 0}
	blockhash := make([]byte, ed25519.PublicKeySize)
	copy(blockhash, []byte("recent-blockhash-placeholder!!"))

	var out []byte
	out = append(out, header...)
	out = append(out, encodeCompactU16(len(accounts))...)
	for _, account := range accounts {
		out = append(out, account...)
	}
	out = append(out, blockhash...)
	out = append(out, encodeCompactU16(0)...)
	out = append(out, encodeCompactU16(0)...)
	return out
}

func testKeypair(label string) (ed25519.PrivateKey, ed25519.PublicKey) {
	seed := sha256.Sum256([]byte(label))
	priv := ed25519.NewKeyFromSeed(seed[:])
	return priv, priv.Public().(ed25519.PublicKey)
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
