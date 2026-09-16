package worker

import "context"

// SolanaRPC confirms on-chain transactions for sweep credit.
type SolanaRPC interface {
	IsConfirmed(ctx context.Context, txSignature string) (bool, error)
}

// fakeSolanaRPC is the locked test double for sweep confirmation.
type fakeSolanaRPC struct {
	confirmed map[string]bool
}

// NewFakeSolanaRPC returns an in-memory RPC client for tests.
func NewFakeSolanaRPC() *fakeSolanaRPC {
	return &fakeSolanaRPC{confirmed: make(map[string]bool)}
}

// Confirm marks a signature confirmed for tests.
func (f *fakeSolanaRPC) Confirm(txSignature string) {
	f.confirmed[txSignature] = true
}

func (f *fakeSolanaRPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	_ = ctx
	return f.confirmed[txSignature], nil
}
