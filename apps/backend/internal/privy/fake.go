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

	validTokens     map[AccessToken]Identity
	memberWallets   map[UserID]WalletRef
	treasuries      map[GroupID]TreasuryRef
	memberBalances  map[string]int64
	treasuryBalances map[string]int64
	lastSweep       SweepRequest
	sweepCount      int
}

// NewFakeClient returns a deterministic in-memory Privy client for tests.
func NewFakeClient() Client {
	return &fakePrivyClient{
		validTokens:      make(map[AccessToken]Identity),
		memberWallets:    make(map[UserID]WalletRef),
		treasuries:       make(map[GroupID]TreasuryRef),
		memberBalances:   make(map[string]int64),
		treasuryBalances: make(map[string]int64),
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

func (f *fakePrivyClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.memberBalances[memberAddress], nil
}

func (f *fakePrivyClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.treasuryBalances[treasuryAddress], nil
}

func (f *fakePrivyClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	_ = ctx
	if req.MemberAddress == "" || req.TreasuryAddress == "" {
		return SweepResult{}, fmt.Errorf("%w: missing addresses", ErrAPI)
	}
	if req.Amount <= 0 {
		return SweepResult{}, fmt.Errorf("%w: invalid amount", ErrAPI)
	}
	if req.RelayerKey == "" {
		return SweepResult{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSweep = req
	f.sweepCount++
	f.memberBalances[req.MemberAddress] -= req.Amount
	if f.memberBalances[req.MemberAddress] < 0 {
		f.memberBalances[req.MemberAddress] = 0
	}
	f.treasuryBalances[req.TreasuryAddress] += req.Amount
	sig := deterministicTxSignature(req.MemberAddress, req.TreasuryAddress, req.Amount, f.sweepCount)
	return SweepResult{TxSignature: sig}, nil
}

// SetMemberUSDCBalance sets fake member wallet USDC for tests.
func SetMemberUSDCBalance(client Client, address string, amount int64) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetMemberUSDCBalance requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.memberBalances[address] = amount
	fake.mu.Unlock()
}

// SetTreasuryUSDCBalance sets fake treasury USDC for tests.
func SetTreasuryUSDCBalance(client Client, address string, amount int64) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetTreasuryUSDCBalance requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.treasuryBalances[address] = amount
	fake.mu.Unlock()
}

// LastSweepRequest returns the most recent sweep submitted to the fake client.
func LastSweepRequest(client Client) (SweepRequest, bool) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		return SweepRequest{}, false
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.sweepCount == 0 {
		return SweepRequest{}, false
	}
	return fake.lastSweep, true
}

func deterministicTxSignature(member, treasury string, amount int64, count int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("sweep:%s:%s:%d:%d", member, treasury, amount, count)))
	return "SWEEP" + hex.EncodeToString(sum[:16])
}

func deterministicPrivyWalletID(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id))
	return "wallet-" + hex.EncodeToString(sum[:8])
}

func deterministicSolanaAddress(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id + ":solana"))
	return "FAKE" + hex.EncodeToString(sum[:16])
}
