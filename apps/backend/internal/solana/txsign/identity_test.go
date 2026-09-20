package txsign

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestSignatureAndBlockhash_legacyAndVersioned(t *testing.T) {
	priv, pub := testKeypair("identity-signer")
	wantHash := make([]byte, ed25519.PublicKeySize)
	copy(wantHash, []byte("recent-blockhash-placeholder!!"))

	messages := map[string][]byte{
		"legacy":    encodeLegacyMessage([][]byte{pub}),
		"versioned": encodeVersionedMessage([][]byte{pub}),
	}
	for name, message := range messages {
		t.Run(name, func(t *testing.T) {
			signed, err := SignAtIndex(encodeTestTransactionFromMessage(message, 1), priv, 0)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}

			signature, blockhash, err := SignatureAndBlockhash(base64.StdEncoding.EncodeToString(signed))
			if err != nil {
				t.Fatalf("SignatureAndBlockhash: %v", err)
			}
			if want := encodeBase58(ed25519.Sign(priv, message)); signature != want {
				t.Fatalf("signature = %q, want %q", signature, want)
			}
			if want := encodeBase58(wantHash); blockhash != want {
				t.Fatalf("blockhash = %q, want %q", blockhash, want)
			}
		})
	}
}

func TestSignatureAndBlockhash_rejectsUntrackableTransactions(t *testing.T) {
	_, pub := testKeypair("identity-unsigned")
	unsigned := encodeTestTransaction([][]byte{pub}, nil)

	cases := map[string]string{
		"not base64":   "%%%",
		"empty":        "",
		"unsigned":     base64.StdEncoding.EncodeToString(unsigned),
		"truncated":    base64.StdEncoding.EncodeToString(unsigned[:70]),
		"opaque bytes": base64.StdEncoding.EncodeToString([]byte("unsigned-buy-tx")),
	}
	for name, tx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := SignatureAndBlockhash(tx); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
