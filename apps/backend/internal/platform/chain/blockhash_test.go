package chain_test

import (
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/errs"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
)

func TestRecentBlockhash_readsLegacyAndVersionedMessages(t *testing.T) {
	t.Parallel()
	hash, key := make([]byte, 32), make([]byte, 32)
	hash[31], key[31] = 7, 1
	for _, prefix := range [][]byte{{}, {0x80}} {
		message := append(append(append([]byte{}, prefix...), 1, 0, 0, 1), key...)
		message = append(message, hash...)
		message = append(message, 0)
		raw := append(append([]byte{1}, make([]byte, 64)...), message...)
		got, err := chain.RecentBlockhash(raw)
		if err != nil || got != chain.EncodeBase58(hash) {
			t.Fatalf("RecentBlockhash = %q, %v", got, err)
		}
	}
	shortMessage := append([]byte{1, 0, 0, 1}, key...)
	short := append(append([]byte{1}, make([]byte, 64)...), shortMessage...)
	for _, raw := range [][]byte{nil, short} {
		if _, err := chain.RecentBlockhash(raw); errs.CodeOf(err) != errs.CodeInvalidInput {
			t.Fatalf("RecentBlockhash(%x) = %v", raw, err)
		}
	}
}
