package signer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

type fakeClient struct {
	mu           sync.Mutex
	seq          int
	relayerSends []RelayerSendRequest
}

// NewFakeClient returns a complete in-memory signer for tests.
func NewFakeClient() Client {
	return &fakeClient{}
}

func (f *fakeClient) Health(ctx context.Context) (HealthInfo, error) {
	_ = ctx
	return HealthInfo{RelayerAddress: "0xrelayer", ChainID: 8453}, nil
}

func (f *fakeClient) CreateWallet(ctx context.Context) (CreatedWallet, error) {
	_ = ctx
	f.mu.Lock()
	f.seq++
	n := f.seq
	f.mu.Unlock()
	id := fmt.Sprintf("wallet-%d", n)
	addr := "0x" + hex.EncodeToString(sha256Sum(id)[:20])
	meta, _ := json.Marshal(map[string]string{"id": id})
	shares, _ := json.Marshal(map[string]string{"share": id})
	return CreatedWallet{WalletID: id, Address: addr, Metadata: meta, KeyShares: shares}, nil
}

func (f *fakeClient) SignTypedData(ctx context.Context, req SignRequest) (string, error) {
	_ = ctx
	sum := sha256.Sum256(append([]byte("sig:"), req.KeyShares...))
	sig := make([]byte, 65)
	copy(sig, sum[:])
	copy(sig[32:], sum[:])
	sig[64] = 27
	return "0x" + hex.EncodeToString(sig), nil
}

func (f *fakeClient) SendTransaction(ctx context.Context, req SendRequest) (string, error) {
	_ = ctx
	f.mu.Lock()
	f.seq++
	n := f.seq
	f.mu.Unlock()
	return "0x" + hex.EncodeToString(sha256Sum(fmt.Sprintf("send:%d", n))[:20]), nil
}

func (f *fakeClient) RelayerSend(ctx context.Context, req RelayerSendRequest) (string, error) {
	_ = ctx
	f.mu.Lock()
	f.seq++
	n := f.seq
	f.relayerSends = append(f.relayerSends, req)
	f.mu.Unlock()
	return "0x" + hex.EncodeToString(sha256Sum(fmt.Sprintf("relay:%d", n))[:20]), nil
}

// RelayerSendCount returns how many relayer sends the fake recorded.
func RelayerSendCount(c Client) int {
	f, ok := c.(*fakeClient)
	if !ok {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.relayerSends)
}

func sha256Sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
