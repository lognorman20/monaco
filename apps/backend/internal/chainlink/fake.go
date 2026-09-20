package chainlink

import (
	"context"
	"fmt"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/marks"
)

type fakeClient struct {
	mu sync.Mutex

	usdcOnly map[string]marks.NavInput
	marked   map[string]marks.NavInput
	errs     map[string]error
}

// NewFakeClient returns an in-memory marks client for tests.
func NewFakeClient() marks.Client {
	return &fakeClient{
		usdcOnly: make(map[string]marks.NavInput),
		marked:   make(map[string]marks.NavInput),
		errs:     make(map[string]error),
	}
}

func treasuryKey(treasury marks.TreasuryRef) string {
	if treasury.GroupID != "" {
		return treasury.GroupID
	}
	return treasury.Address
}

// RegisterUSDCOnlyPot configures USDCOnlyPot for a treasury key.
func RegisterUSDCOnlyPot(client marks.Client, treasury marks.TreasuryRef, input marks.NavInput) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("chainlink: RegisterUSDCOnlyPot requires NewFakeClient")
	}
	f.mu.Lock()
	f.usdcOnly[treasuryKey(treasury)] = input
	delete(f.errs, treasuryKey(treasury)+":usdc")
	f.mu.Unlock()
}

// RegisterMarkedPot configures MarkedPot for a treasury key.
func RegisterMarkedPot(client marks.Client, treasury marks.TreasuryRef, input marks.NavInput) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("chainlink: RegisterMarkedPot requires NewFakeClient")
	}
	f.mu.Lock()
	f.marked[treasuryKey(treasury)] = input
	delete(f.errs, treasuryKey(treasury)+":marked")
	f.mu.Unlock()
}

// RegisterMarkedPotError forces MarkedPot to return err for a treasury key.
func RegisterMarkedPotError(client marks.Client, treasury marks.TreasuryRef, err error) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("chainlink: RegisterMarkedPotError requires NewFakeClient")
	}
	f.mu.Lock()
	f.errs[treasuryKey(treasury)+":marked"] = err
	f.mu.Unlock()
}

func (f *fakeClient) USDCOnlyPot(ctx context.Context, treasury marks.TreasuryRef) (marks.NavInput, error) {
	_ = ctx
	key := treasuryKey(treasury)
	f.mu.Lock()
	if err, ok := f.errs[key+":usdc"]; ok {
		f.mu.Unlock()
		return marks.NavInput{}, err
	}
	input, ok := f.usdcOnly[key]
	f.mu.Unlock()
	if !ok {
		return marks.NavInput{TreasuryUsdc: treasury.TreasuryUsdc}, nil
	}
	if input.TreasuryUsdc == 0 {
		input.TreasuryUsdc = treasury.TreasuryUsdc
	}
	return input, nil
}

func (f *fakeClient) MarkedPot(ctx context.Context, treasury marks.TreasuryRef, holdings []marks.CostBasis) (marks.NavInput, error) {
	_ = ctx
	_ = holdings
	key := treasuryKey(treasury)
	f.mu.Lock()
	if err, ok := f.errs[key+":marked"]; ok {
		f.mu.Unlock()
		return marks.NavInput{}, err
	}
	input, ok := f.marked[key]
	f.mu.Unlock()
	if !ok {
		return marks.NavInput{}, fmt.Errorf("chainlink: marked pot not configured for %s", key)
	}
	if input.TreasuryUsdc == 0 {
		input.TreasuryUsdc = treasury.TreasuryUsdc
	}
	return input, nil
}
