package privy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// fakePrivyClient is the locked test double for M1 handler and integration tests.
type fakePrivyClient struct {
	mu sync.Mutex

	validTokens map[AccessToken]Identity
	memberWallets map[UserID]WalletRef
	treasuries    map[GroupID]TreasuryRef
}

// NewFakeClient returns a deterministic in-memory Privy client for tests.
func NewFakeClient() Client {
	return &fakePrivyClient{
		validTokens:   make(map[AccessToken]Identity),
		memberWallets: make(map[UserID]WalletRef),
		treasuries:    make(map[GroupID]TreasuryRef),
	}
}

// RegisterToken maps an access token to an identity for VerifySession tests.
func RegisterToken(client Client, token AccessToken, identity Identity) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: RegisterToken requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.validTokens[token] = identity
	fake.mu.Unlock()
}

func (f *fakePrivyClient) VerifySession(ctx context.Context, token AccessToken) (Identity, error) {
	_ = ctx
	f.mu.Lock()
	identity, ok := f.validTokens[token]
	f.mu.Unlock()
	if !ok || identity.PrivyUserID == "" {
		return Identity{}, ErrInvalidToken
	}
	return identity, nil
}

func (f *fakePrivyClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID UserID) (WalletRef, error) {
	_ = ctx
	_ = privyUserID
	if string(userID) == "" {
		return WalletRef{}, fmt.Errorf("%w: missing monaco user id", ErrAPI)
	}

	f.mu.Lock()
	if existing, ok := f.memberWallets[userID]; ok {
		f.mu.Unlock()
		return existing, nil
	}

	ref := WalletRef{
		UserID:        userID,
		PrivyWalletID: deterministicPrivyWalletID("member", string(userID)),
		SolanaAddress: deterministicSolanaAddress("member", string(userID)),
	}
	f.memberWallets[userID] = ref
	f.mu.Unlock()
	return ref, nil
}

func (f *fakePrivyClient) EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error) {
	_ = ctx
	if string(groupID) == "" {
		return TreasuryRef{}, fmt.Errorf("%w: missing group id", ErrAPI)
	}

	f.mu.Lock()
	if existing, ok := f.treasuries[groupID]; ok {
		f.mu.Unlock()
		return existing, nil
	}

	ref := TreasuryRef{
		GroupID:       groupID,
		PrivyWalletID: deterministicPrivyWalletID("treasury", string(groupID)),
		SolanaAddress: deterministicSolanaAddress("treasury", string(groupID)),
	}
	f.treasuries[groupID] = ref
	f.mu.Unlock()
	return ref, nil
}

func deterministicPrivyWalletID(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id))
	return "wallet-" + hex.EncodeToString(sum[:8])
}

func deterministicSolanaAddress(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id + ":solana"))
	return "FAKE" + hex.EncodeToString(sum[:16])
}
