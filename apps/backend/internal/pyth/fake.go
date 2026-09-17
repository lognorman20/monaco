package pyth

import (
	"context"
	"fmt"
	"sync"
)

// fakePythClient is the locked test double for marked pot and view tests.
type fakePythClient struct {
	mu sync.Mutex

	usdcOnly map[string]NavInput
	marked   map[string]NavInput
	errs     map[string]error
}

// NewFakeClient returns an in-memory Pyth client for tests.
func NewFakeClient() Client {
	return &fakePythClient{
		usdcOnly: make(map[string]NavInput),
		marked:   make(map[string]NavInput),
		errs:     make(map[string]error),
	}
}

func treasuryKey(treasury TreasuryRef) string {
	if treasury.GroupID != "" {
		return treasury.GroupID
	}
	return treasury.Address
}

// RegisterUSDCOnlyPot configures USDCOnlyPot for a treasury key.
func RegisterUSDCOnlyPot(client Client, treasury TreasuryRef, input NavInput) {
	fake, ok := client.(*fakePythClient)
	if !ok {
		panic("pyth: RegisterUSDCOnlyPot requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.usdcOnly[treasuryKey(treasury)] = input
	delete(fake.errs, treasuryKey(treasury)+":usdc")
	fake.mu.Unlock()
}

// RegisterMarkedPot configures MarkedPot for a treasury key.
func RegisterMarkedPot(client Client, treasury TreasuryRef, input NavInput) {
	fake, ok := client.(*fakePythClient)
	if !ok {
		panic("pyth: RegisterMarkedPot requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.marked[treasuryKey(treasury)] = input
	delete(fake.errs, treasuryKey(treasury)+":marked")
	fake.mu.Unlock()
}

// RegisterMarkedPotError forces MarkedPot to return err for a treasury key.
func RegisterMarkedPotError(client Client, treasury TreasuryRef, err error) {
	fake, ok := client.(*fakePythClient)
	if !ok {
		panic("pyth: RegisterMarkedPotError requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.errs[treasuryKey(treasury)+":marked"] = err
	fake.mu.Unlock()
}

func (f *fakePythClient) USDCOnlyPot(ctx context.Context, treasury TreasuryRef) (NavInput, error) {
	_ = ctx
	key := treasuryKey(treasury)
	f.mu.Lock()
	if err, ok := f.errs[key+":usdc"]; ok {
		f.mu.Unlock()
		return NavInput{}, err
	}
	input, ok := f.usdcOnly[key]
	f.mu.Unlock()
	if !ok {
		return NavInput{TreasuryUsdc: treasury.TreasuryUsdc}, nil
	}
	if input.TreasuryUsdc == 0 {
		input.TreasuryUsdc = treasury.TreasuryUsdc
	}
	return input, nil
}

func (f *fakePythClient) MarkedPot(ctx context.Context, treasury TreasuryRef, holdings []CostBasis) (NavInput, error) {
	_ = ctx
	_ = holdings
	key := treasuryKey(treasury)
	f.mu.Lock()
	if err, ok := f.errs[key+":marked"]; ok {
		f.mu.Unlock()
		return NavInput{}, err
	}
	input, ok := f.marked[key]
	f.mu.Unlock()
	if !ok {
		return NavInput{}, fmt.Errorf("pyth: marked pot not configured for %s", key)
	}
	if input.TreasuryUsdc == 0 {
		input.TreasuryUsdc = treasury.TreasuryUsdc
	}
	if !input.AfterHours {
		input.AfterHours = PotAfterHours(input.Holdings)
	}
	return input, nil
}
