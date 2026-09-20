package worker

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/evm"
)

// Confirmer confirms EVM transactions for sweep and withdrawal credit.
type Confirmer interface {
	IsConfirmed(ctx context.Context, txHash string) (bool, error)
}

type evmConfirmer struct {
	chain evm.Client
}

// NewEVMConfirmer wraps an EVM client as a Confirmer.
func NewEVMConfirmer(chain evm.Client) Confirmer {
	return &evmConfirmer{chain: chain}
}

func (c *evmConfirmer) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	return c.chain.IsConfirmed(ctx, txHash)
}

type fakeConfirmer struct {
	confirmed map[string]bool
}

// NewFakeConfirmer returns an in-memory confirmer for tests.
func NewFakeConfirmer() *fakeConfirmer {
	return &fakeConfirmer{confirmed: make(map[string]bool)}
}

// Confirm marks txHash as confirmed for tests.
func (f *fakeConfirmer) Confirm(txHash string) {
	f.confirmed[txHash] = true
}

func (f *fakeConfirmer) IsConfirmed(ctx context.Context, txHash string) (bool, error) {
	_ = ctx
	return f.confirmed[txHash], nil
}
