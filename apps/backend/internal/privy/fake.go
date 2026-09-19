package privy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// fakePrivyClient is the locked test double for M1 handler and integration tests.
type fakePrivyClient struct {
	mu sync.Mutex

	validTokens     map[AccessToken]Identity
	memberWallets   map[UserID]WalletRef
	privyUserWallets map[string]WalletRef
	treasuries      map[GroupID]TreasuryRef
	memberBalances  map[string]int64
	treasuryBalances map[string]int64
	lastSweep       SweepRequest
	sweepCount      int
	lastTransfer    TransferRequest
	transferCount   int
	rejectSubmitTransfer bool
	rejectSubmitTransferErr error
	validProofs     map[string]struct{}
	lastPayout      PayUSDCRequest
	payoutCount     int
	rejectProofs    bool
	rejectSubmitSweep bool
	rejectSubmitSweepErr error
}

// NewFakeClient returns a deterministic in-memory Privy client for tests.
func NewFakeClient() Client {
	return &fakePrivyClient{
		validTokens:      make(map[AccessToken]Identity),
		memberWallets:    make(map[UserID]WalletRef),
		privyUserWallets: make(map[string]WalletRef),
		treasuries:       make(map[GroupID]TreasuryRef),
		memberBalances:   make(map[string]int64),
		treasuryBalances: make(map[string]int64),
		validProofs:      make(map[string]struct{}),
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
		logFake("verify_session", "ok", false)
		return Identity{}, ErrInvalidToken
	}
	logFake("verify_session", "ok", true, "privy_user_id", identity.PrivyUserID)
	return identity, nil
}

// RegisterPrivyMemberWallet seeds an existing Privy Solana wallet for tests.
func RegisterPrivyMemberWallet(client Client, privyUserID string, ref WalletRef) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: RegisterPrivyMemberWallet requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.privyUserWallets[privyUserID] = ref
	fake.mu.Unlock()
}

func (f *fakePrivyClient) EnsureMemberWallet(ctx context.Context, privyUserID string, userID UserID) (WalletRef, error) {
	_ = ctx
	if string(userID) == "" {
		return WalletRef{}, fmt.Errorf("%w: missing monaco user id", ErrAPI)
	}

	f.mu.Lock()
	if existing, ok := f.memberWallets[userID]; ok {
		f.mu.Unlock()
		logFake("ensure_member_wallet", "user_id", userID, "cached", true)
		return existing, nil
	}
	if existing, ok := f.privyUserWallets[privyUserID]; ok {
		ref := existing
		ref.UserID = userID
		f.memberWallets[userID] = ref
		f.mu.Unlock()
		logFake("ensure_member_wallet", "user_id", userID, "cached", true, "wallet_id", ref.PrivyWalletID, "source", "privy")
		return ref, nil
	}

	ref := WalletRef{
		UserID:        userID,
		PrivyWalletID: deterministicPrivyWalletID("member", string(userID)),
		SolanaAddress: deterministicSolanaAddress("member", string(userID)),
	}
	f.memberWallets[userID] = ref
	if privyUserID != "" {
		f.privyUserWallets[privyUserID] = ref
	}
	f.mu.Unlock()
	logFake("ensure_member_wallet", "user_id", userID, "cached", false, "wallet_id", ref.PrivyWalletID)
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
		logFake("ensure_treasury", "group_id", groupID, "cached", true)
		return existing, nil
	}

	ref := TreasuryRef{
		GroupID:       groupID,
		PrivyWalletID: deterministicPrivyWalletID("treasury", string(groupID)),
		SolanaAddress: deterministicSolanaAddress("treasury", string(groupID)),
	}
	f.treasuries[groupID] = ref
	f.mu.Unlock()
	logFake("ensure_treasury", "group_id", groupID, "cached", false, "wallet_id", ref.PrivyWalletID)
	return ref, nil
}

func (f *fakePrivyClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	balance := f.memberBalances[memberAddress]
	logFake("member_usdc_balance", "address", memberAddress, "balance", balance)
	return balance, nil
}

func (f *fakePrivyClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	balance := f.treasuryBalances[treasuryAddress]
	logFake("treasury_usdc_balance", "address", treasuryAddress, "balance", balance)
	return balance, nil
}

func (f *fakePrivyClient) VerifyPayoutProof(ctx context.Context, userID string, proof PayoutProof) error {
	_ = ctx
	if f.rejectProofs {
		return ErrInvalidPayoutProof
	}
	if strings.TrimSpace(proof.PayoutAddress) == "" {
		return ErrInvalidPayoutProof
	}
	expected := PayoutMessage(userID, proof.PayoutAddress)
	if proof.Message != expected {
		return ErrInvalidPayoutProof
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.validProofs[proof.Signature]; ok {
		return nil
	}
	if proof.Signature == deterministicPayoutSignature(userID, proof.PayoutAddress) {
		return nil
	}
	return ErrInvalidPayoutProof
}

func (f *fakePrivyClient) PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error) {
	_ = ctx
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryAddress == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	logFake("pay_usdc", "amount", req.Amount, "to", req.ToAddress)
	f.lastPayout = req
	f.payoutCount++
	balance := f.treasuryBalances[req.TreasuryAddress]
	if balance < req.Amount {
		return PayUSDCResult{}, fmt.Errorf("%w: insufficient treasury usdc", ErrAPI)
	}
	f.treasuryBalances[req.TreasuryAddress] = balance - req.Amount
	f.memberBalances[req.ToAddress] += req.Amount
	sig := deterministicTxSignature(req.TreasuryAddress, req.ToAddress, req.Amount, f.payoutCount)
	return PayUSDCResult{TxSignature: sig}, nil
}

func (f *fakePrivyClient) SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	_ = ctx
	if req.MemberAddress == "" || req.ToAddress == "" {
		return TransferResult{}, fmt.Errorf("%w: missing addresses", ErrAPI)
	}
	if req.Amount <= 0 {
		return TransferResult{}, fmt.Errorf("%w: invalid amount", ErrAPI)
	}
	if req.RelayerKey == "" {
		return TransferResult{}, fmt.Errorf("%w: relayer key required", ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectSubmitTransfer {
		err := f.rejectSubmitTransferErr
		if err == nil {
			err = fmt.Errorf("%w: submit transfer rejected", ErrAPI)
		}
		return TransferResult{}, err
	}
	logFake("submit_member_usdc_transfer", "amount", req.Amount, "member", req.MemberAddress, "to", req.ToAddress)
	f.lastTransfer = req
	f.transferCount++
	f.memberBalances[req.MemberAddress] -= req.Amount
	if f.memberBalances[req.MemberAddress] < 0 {
		f.memberBalances[req.MemberAddress] = 0
	}
	sig := deterministicTxSignature(req.MemberAddress, req.ToAddress, req.Amount, f.transferCount)
	return TransferResult{TxSignature: sig}, nil
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
	if f.rejectSubmitSweep {
		err := f.rejectSubmitSweepErr
		if err == nil {
			err = fmt.Errorf("%w: submit sweep rejected", ErrAPI)
		}
		return SweepResult{}, err
	}
	logFake("submit_sweep", "amount", req.Amount, "member", req.MemberAddress, "treasury", req.TreasuryAddress)
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
// RegisterPayoutProof registers a valid payout proof signature for tests.
func RegisterPayoutProof(client Client, signature string) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: RegisterPayoutProof requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.validProofs[signature] = struct{}{}
	fake.mu.Unlock()
}

// SetRejectSubmitSweep forces SubmitSweep to fail for tests.
func SetRejectSubmitSweep(client Client, reject bool, err error) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetRejectSubmitSweep requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.rejectSubmitSweep = reject
	fake.rejectSubmitSweepErr = err
	fake.mu.Unlock()
}

// SetRejectPayoutProofs forces VerifyPayoutProof to fail for tests.
func SetRejectPayoutProofs(client Client, reject bool) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetRejectPayoutProofs requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.rejectProofs = reject
	fake.mu.Unlock()
}

// LastPayUSDCRequest returns the most recent payout submitted to the fake client.
func LastPayUSDCRequest(client Client) (PayUSDCRequest, bool) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		return PayUSDCRequest{}, false
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.payoutCount == 0 {
		return PayUSDCRequest{}, false
	}
	return fake.lastPayout, true
}

// BuildValidPayoutProof returns a proof that passes fake VerifyPayoutProof.
func BuildValidPayoutProof(userID, payoutAddress string) PayoutProof {
	message := PayoutMessage(userID, payoutAddress)
	return PayoutProof{
		PayoutAddress: payoutAddress,
		Message:       message,
		Signature:     deterministicPayoutSignature(userID, payoutAddress),
	}
}

// SetRejectSubmitTransfer forces SubmitMemberUSDCTransfer to fail for tests.
func SetRejectSubmitTransfer(client Client, reject bool, err error) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetRejectSubmitTransfer requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.rejectSubmitTransfer = reject
	fake.rejectSubmitTransferErr = err
	fake.mu.Unlock()
}

// LastTransferRequest returns the most recent member USDC transfer submitted to the fake client.
func LastTransferRequest(client Client) (TransferRequest, bool) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		return TransferRequest{}, false
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.transferCount == 0 {
		return TransferRequest{}, false
	}
	return fake.lastTransfer, true
}

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
