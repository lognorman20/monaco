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
	prepareCount    int
	sweepLastValidBlockHeight uint64
	lastTransfer    TransferRequest
	transferCount   int
	rejectSubmitTransfer bool
	rejectSubmitTransferErr error
	forcedTransferSignature string
	validProofs     map[string]struct{}
	lastPayout      PayUSDCRequest
	payoutCount     int
	preparedPayoutCount int
	payouts         map[string]*fakePayout
	payoutBehavior  FakePayoutBehavior
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
		payouts:          make(map[string]*fakePayout),
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

// fakePayout is one prepared treasury payout and what the fake chain did with it.
type fakePayout struct {
	req     PayUSDCRequest
	state   PayoutState
	reason  string
	reached bool
	expired bool
}

// FakePayoutBehavior scripts what the fake chain does with treasury payouts.
type FakePayoutBehavior struct {
	// PrepareErr fails PrepareUSDCPayout before anything is signed.
	PrepareErr error
	// BroadcastErr is returned by BroadcastUSDCPayout. The transfer still reaches the chain
	// unless BroadcastLost is set: a timeout does not mean the cluster never saw it.
	BroadcastErr error
	// BroadcastLost means a broadcast never reaches the cluster.
	BroadcastLost bool
	// FailOnChain lands the transfer with an on-chain error. No USDC moves.
	FailOnChain bool
	// StatusErr fails USDCPayoutStatus, as an RPC outage would.
	StatusErr error
}

func (f *fakePrivyClient) PrepareUSDCPayout(ctx context.Context, req PayUSDCRequest) (PreparedPayout, error) {
	_ = ctx
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryAddress == "" {
		return PreparedPayout{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.payoutBehavior.PrepareErr != nil {
		return PreparedPayout{}, f.payoutBehavior.PrepareErr
	}
	f.preparedPayoutCount++
	sig := deterministicTxSignature(req.TreasuryAddress, req.ToAddress, req.Amount, f.preparedPayoutCount)
	f.payouts[sig] = &fakePayout{req: req, state: PayoutStatePending}
	logFake("prepare_usdc_payout", "amount", req.Amount, "to", req.ToAddress, "tx_signature", sig)
	return PreparedPayout{
		TxSignature:          sig,
		SignedTransaction:    "SIGNED:" + sig,
		LastValidBlockHeight: int64(1000 + f.preparedPayoutCount),
	}, nil
}

func (f *fakePrivyClient) BroadcastUSDCPayout(ctx context.Context, prepared PreparedPayout) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	payout, ok := f.payouts[prepared.TxSignature]
	if !ok || prepared.SignedTransaction != "SIGNED:"+prepared.TxSignature {
		return fmt.Errorf("%w: unknown signed payout", ErrAPI)
	}
	logFake("broadcast_usdc_payout", "amount", payout.req.Amount, "to", payout.req.ToAddress, "tx_signature", prepared.TxSignature)

	if payout.expired {
		return fmt.Errorf("%w: blockhash not found", ErrAPI)
	}
	// A signature lands at most once, however often it is broadcast.
	if !payout.reached && !f.payoutBehavior.BroadcastLost {
		payout.reached = true
		balance := f.treasuryBalances[payout.req.TreasuryAddress]
		switch {
		case f.payoutBehavior.FailOnChain:
			payout.state, payout.reason = PayoutStateFailed, "InstructionError: scripted failure"
		case balance < payout.req.Amount:
			payout.state, payout.reason = PayoutStateFailed, "InstructionError: insufficient funds"
		default:
			payout.state = PayoutStateConfirmed
			f.treasuryBalances[payout.req.TreasuryAddress] = balance - payout.req.Amount
			f.memberBalances[payout.req.ToAddress] += payout.req.Amount
			f.lastPayout = payout.req
			f.payoutCount++
		}
	}
	return f.payoutBehavior.BroadcastErr
}

func (f *fakePrivyClient) USDCPayoutStatus(ctx context.Context, prepared PreparedPayout) (PayoutStatus, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.payoutBehavior.StatusErr != nil {
		return PayoutStatus{}, f.payoutBehavior.StatusErr
	}
	payout, ok := f.payouts[prepared.TxSignature]
	if ok && payout.reached {
		return PayoutStatus{State: payout.state, Reason: payout.reason}, nil
	}
	if ok && payout.expired {
		return PayoutStatus{State: PayoutStateDropped}, nil
	}
	return PayoutStatus{State: PayoutStatePending}, nil
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
	sig := f.forcedTransferSignature
	if sig == "" {
		sig = deterministicTxSignature(req.MemberAddress, req.ToAddress, req.Amount, f.transferCount)
	}
	return TransferResult{TxSignature: sig}, nil
}

func (f *fakePrivyClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	prepared, err := f.PrepareSweep(ctx, req)
	if err != nil {
		return SweepResult{}, err
	}
	return f.BroadcastSweep(ctx, prepared)
}

func (f *fakePrivyClient) PrepareSweep(ctx context.Context, req SweepRequest) (PreparedSweep, error) {
	_ = ctx
	if req.MemberAddress == "" || req.TreasuryAddress == "" {
		return PreparedSweep{}, fmt.Errorf("%w: %w: missing addresses", ErrBroadcastRejected, ErrAPI)
	}
	if req.Amount <= 0 {
		return PreparedSweep{}, fmt.Errorf("%w: %w: invalid amount", ErrBroadcastRejected, ErrAPI)
	}
	if req.RelayerKey == "" {
		return PreparedSweep{}, fmt.Errorf("%w: %w: relayer key required", ErrBroadcastRejected, ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepareCount++
	logFake("prepare_sweep", "amount", req.Amount, "member", req.MemberAddress, "treasury", req.TreasuryAddress)
	return PreparedSweep{
		Request:              req,
		WalletID:             deterministicPrivyWalletID("member-address", req.MemberAddress),
		TransactionBase64:    "fake-sweep-transaction",
		TxSignature:          deterministicTxSignature(req.MemberAddress, req.TreasuryAddress, req.Amount, f.prepareCount),
		LastValidBlockHeight: f.sweepLastValidBlockHeight,
	}, nil
}

func (f *fakePrivyClient) BroadcastSweep(ctx context.Context, prepared PreparedSweep) (SweepResult, error) {
	_ = ctx
	req := prepared.Request
	if prepared.TxSignature == "" {
		return SweepResult{}, fmt.Errorf("%w: %w: sweep is not prepared", ErrBroadcastRejected, ErrAPI)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectSubmitSweep {
		err := f.rejectSubmitSweepErr
		if err == nil {
			err = fmt.Errorf("%w: %w: submit sweep rejected", ErrBroadcastRejected, ErrAPI)
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
	return SweepResult{TxSignature: prepared.TxSignature}, nil
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

// SweepCount returns how many sweeps the fake client broadcast.
func SweepCount(client Client) int {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SweepCount requires NewFakeClient")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.sweepCount
}

// SetSweepLastValidBlockHeight sets the expiry height reported by PrepareSweep for tests.
func SetSweepLastValidBlockHeight(client Client, height uint64) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetSweepLastValidBlockHeight requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.sweepLastValidBlockHeight = height
	fake.mu.Unlock()
}

// SetRejectSubmitSweep forces SubmitSweep and BroadcastSweep to fail for tests.
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

// SetPayoutBehavior scripts how the fake chain treats treasury payouts from now on.
func SetPayoutBehavior(client Client, behavior FakePayoutBehavior) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetPayoutBehavior requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.payoutBehavior = behavior
	fake.mu.Unlock()
}

// ExpirePendingPayouts moves the fake chain past the blockhash of every prepared payout that
// has not reached it: those can never land now and report as dropped.
func ExpirePendingPayouts(client Client) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: ExpirePendingPayouts requires NewFakeClient")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, payout := range fake.payouts {
		if !payout.reached {
			payout.expired = true
		}
	}
}

// LandedPayoutCount returns how many treasury payouts moved USDC on the fake chain.
func LandedPayoutCount(client Client) int {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		return 0
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.payoutCount
}

// LastPayUSDCRequest returns the most recent payout that moved USDC on the fake chain.
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

// SetForcedTransferSignature forces SubmitMemberUSDCTransfer to return a fixed signature for tests.
func SetForcedTransferSignature(client Client, signature string) {
	fake, ok := client.(*fakePrivyClient)
	if !ok {
		panic("privy: SetForcedTransferSignature requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.forcedTransferSignature = signature
	fake.mu.Unlock()
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
