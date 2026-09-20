package swapchain

import (
	"context"
	"fmt"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// FakeReader is the in-memory swapprovider.ChainReader for tests. An unregistered signature
// is not found and every blockhash is still valid, so an unconfigured fake never lets a
// swap be declared failed.
type FakeReader struct {
	mu sync.Mutex

	statuses          map[string]swapprovider.SignatureStatus
	changes           map[string]map[string]int64
	landAll           map[string]int64
	blockhashesValid  bool
	err               error
	statusCalls       int
	onBlockhashLookup func()
}

// NewFakeReader returns a chain on which nothing has landed and nothing has expired.
func NewFakeReader() *FakeReader {
	return &FakeReader{
		statuses:         make(map[string]swapprovider.SignatureStatus),
		changes:          make(map[string]map[string]int64),
		blockhashesValid: true,
	}
}

// Land records signature as finalized with the given per-mint balance changes for any owner.
func (f *FakeReader) Land(signature string, changes map[string]int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[signature] = swapprovider.SignatureStatus{Found: true, Finalized: true}
	f.changes[signature] = changes
}

// LandEverything makes every signature finalized with the same balance changes. Tests use it
// when they cannot know the signature the treasury signer will produce.
func (f *FakeReader) LandEverything(changes map[string]int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landAll = changes
}

// SetStatus records an arbitrary status for signature.
func (f *FakeReader) SetStatus(signature string, status swapprovider.SignatureStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[signature] = status
}

// ExpireBlockhashes makes every blockhash too old to land.
func (f *FakeReader) ExpireBlockhashes() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockhashesValid = false
}

// SetError makes every call fail with err. Nil restores normal behaviour.
func (f *FakeReader) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// OnBlockhashLookup runs fn during each IsBlockhashValid call, before it answers.
func (f *FakeReader) OnBlockhashLookup(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onBlockhashLookup = fn
}

// StatusCalls reports how many SignatureStatus calls were made.
func (f *FakeReader) StatusCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusCalls
}

// SignatureStatus implements swapprovider.ChainReader.
func (f *FakeReader) SignatureStatus(ctx context.Context, signature string) (swapprovider.SignatureStatus, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls++
	if f.err != nil {
		return swapprovider.SignatureStatus{}, f.err
	}
	if status, ok := f.statuses[signature]; ok {
		return status, nil
	}
	if f.landAll != nil {
		return swapprovider.SignatureStatus{Found: true, Finalized: true}, nil
	}
	return swapprovider.SignatureStatus{}, nil
}

// IsBlockhashValid implements swapprovider.ChainReader.
func (f *FakeReader) IsBlockhashValid(ctx context.Context, blockhash string) (bool, error) {
	_ = ctx
	f.mu.Lock()
	hook := f.onBlockhashLookup
	f.mu.Unlock()
	if hook != nil {
		hook()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if blockhash == "" {
		return false, fmt.Errorf("swapchain: blockhash is required")
	}
	return f.blockhashesValid, nil
}

// TokenBalanceChanges implements swapprovider.ChainReader.
func (f *FakeReader) TokenBalanceChanges(ctx context.Context, signature, owner string) (map[string]int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if owner == "" {
		return nil, fmt.Errorf("swapchain: owner is required")
	}
	if changes, ok := f.changes[signature]; ok {
		return changes, nil
	}
	if f.landAll != nil {
		return f.landAll, nil
	}
	return nil, fmt.Errorf("swapchain: transaction %s has no finalized metadata", signature)
}
