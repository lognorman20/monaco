package key

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
)

func testRelayerKeypair(t *testing.T) (base58Key string, seed []byte) {
	t.Helper()
	sum := sha256.Sum256([]byte("relayer-wallet"))
	seed = sum[:]
	priv := ed25519.NewKeyFromSeed(seed)
	return encodeBase58(priv), seed
}

func TestPublicKeyBase58FromPrivateKey_matchesKeypair(t *testing.T) {
	base58Key, seed := testRelayerKeypair(t)
	want := encodeBase58(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))

	got, err := PublicKeyBase58FromPrivateKey(base58Key)
	if err != nil {
		t.Fatalf("PublicKeyBase58FromPrivateKey: %v", err)
	}
	if got != want {
		t.Fatalf("PublicKeyBase58FromPrivateKey = %q, want %q", got, want)
	}
}

func TestParsePrivateKey_base58SecretKey(t *testing.T) {
	base58Key, _ := testRelayerKeypair(t)

	got, err := ParsePrivateKey(base58Key)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if got != base58Key {
		t.Fatalf("ParsePrivateKey = %q, want %q", got, base58Key)
	}
}

func TestParsePrivateKey_quotedBase58(t *testing.T) {
	base58Key, _ := testRelayerKeypair(t)

	got, err := ParsePrivateKey(`"` + base58Key + `"`)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	if got != base58Key {
		t.Fatalf("ParsePrivateKey = %q, want %q", got, base58Key)
	}
}

func TestParsePrivateKey_jsonIntArray(t *testing.T) {
	_, seed := testRelayerKeypair(t)
	priv := ed25519.NewKeyFromSeed(seed)
	ints := make([]int, len(priv))
	for i, b := range priv {
		ints[i] = int(b)
	}
	jsonKey, err := json.Marshal(ints)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := ParsePrivateKey(string(jsonKey))
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	want, _ := testRelayerKeypair(t)
	if got != want {
		t.Fatalf("ParsePrivateKey = %q, want %q", got, want)
	}
}

func TestParsePrivateKey_bracketValue_returnsHelpfulError(t *testing.T) {
	_, err := ParsePrivateKey("[not,a,valid,key]")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("error = %q, want JSON array guidance", err.Error())
	}
}

func TestParsePrivateKey_invalidBase58WithBracket_returnsHelpfulError(t *testing.T) {
	_, err := ParsePrivateKey("[1,2,3")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("error = %q, want JSON array guidance", err.Error())
	}
}
