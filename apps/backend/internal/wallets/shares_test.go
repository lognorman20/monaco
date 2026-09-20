package wallets

import "testing"

func TestShares_roundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 3)
	}
	plain := []byte(`{"share":"abc"}`)
	enc, err := EncryptShares(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptShares(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("got %s want %s", got, plain)
	}
}
