package wallets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
)

type fakeClient struct {
	mu sync.Mutex

	memberWallets        map[UserID]WalletRef
	dynamicUserWallets   map[string]WalletRef
	treasuries           map[GroupID]TreasuryRef
	memberBalances       map[string]int64
	treasuryBalances     map[string]int64
	lastSweep            SweepRequest
	sweepCount           int
	lastTransfer         TransferRequest
	transferCount        int
	rejectSubmitTransfer bool
	rejectSubmitTransferErr error
	forcedTransferHash   string
	lastPayout           PayUSDCRequest
	payoutCount          int
	rejectSubmitSweep    bool
	rejectSubmitSweepErr error
	treasuryTxCount      int
	lastTreasuryTx       treasuryTxRecord
}

type treasuryTxRecord struct {
	treasury TreasuryRef
	to       string
	data     []byte
	valueWei *big.Int
}

// NewFakeClient returns a deterministic in-memory wallets client for tests.
func NewFakeClient() Client {
	return &fakeClient{
		memberWallets:      make(map[UserID]WalletRef),
		dynamicUserWallets: make(map[string]WalletRef),
		treasuries:         make(map[GroupID]TreasuryRef),
		memberBalances:     make(map[string]int64),
		treasuryBalances:   make(map[string]int64),
	}
}

func RegisterDynamicMemberWallet(client Client, dynamicUserID string, ref WalletRef) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: RegisterDynamicMemberWallet requires NewFakeClient")
	}
	f.mu.Lock()
	f.dynamicUserWallets[dynamicUserID] = ref
	f.mu.Unlock()
}

func (f *fakeClient) EnsureMemberWallet(ctx context.Context, dynamicUserID string, userID UserID) (WalletRef, error) {
	_ = ctx
	if string(userID) == "" {
		return WalletRef{}, fmt.Errorf("%w: missing monaco user id", ErrAPI)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.memberWallets[userID]; ok {
		return existing, nil
	}
	if existing, ok := f.dynamicUserWallets[dynamicUserID]; ok {
		ref := existing
		ref.UserID = userID
		f.memberWallets[userID] = ref
		return ref, nil
	}
	ref := WalletRef{
		UserID:   userID,
		WalletID: deterministicWalletID("member", string(userID)),
		Address:  deterministicEVMAddress("member", string(userID)),
	}
	f.memberWallets[userID] = ref
	if dynamicUserID != "" {
		f.dynamicUserWallets[dynamicUserID] = ref
	}
	return ref, nil
}

func (f *fakeClient) EnsureTreasury(ctx context.Context, groupID GroupID) (TreasuryRef, error) {
	_ = ctx
	if string(groupID) == "" {
		return TreasuryRef{}, fmt.Errorf("%w: missing group id", ErrAPI)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.treasuries[groupID]; ok {
		return existing, nil
	}
	ref := TreasuryRef{
		GroupID:  groupID,
		WalletID: deterministicWalletID("treasury", string(groupID)),
		Address:  deterministicEVMAddress("treasury", string(groupID)),
	}
	f.treasuries[groupID] = ref
	return ref, nil
}

func (f *fakeClient) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.memberBalances[memberAddress], nil
}

func (f *fakeClient) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.treasuryBalances[treasuryAddress], nil
}

func (f *fakeClient) PayUSDC(ctx context.Context, req PayUSDCRequest) (PayUSDCResult, error) {
	_ = ctx
	treasuryAddr := req.TreasuryAddress
	if treasuryAddr == "" {
		treasuryAddr = req.TreasuryRef.Address
	}
	if req.Amount <= 0 || req.ToAddress == "" || treasuryAddr == "" {
		return PayUSDCResult{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPayout = req
	f.payoutCount++
	balance := f.treasuryBalances[treasuryAddr]
	if balance < req.Amount {
		return PayUSDCResult{}, fmt.Errorf("%w: insufficient treasury usdc", ErrAPI)
	}
	f.treasuryBalances[treasuryAddr] = balance - req.Amount
	f.memberBalances[req.ToAddress] += req.Amount
	hash := deterministicTxHash(treasuryAddr, req.ToAddress, req.Amount, f.payoutCount)
	return PayUSDCResult{TxHash: hash}, nil
}

func (f *fakeClient) SubmitMemberUSDCTransfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	_ = ctx
	if req.MemberAddress == "" || req.ToAddress == "" || req.Amount <= 0 {
		return TransferResult{}, fmt.Errorf("%w: invalid transfer", ErrAPI)
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
	f.lastTransfer = req
	f.transferCount++
	f.memberBalances[req.MemberAddress] -= req.Amount
	hash := f.forcedTransferHash
	if hash == "" {
		hash = deterministicTxHash(req.MemberAddress, req.ToAddress, req.Amount, f.transferCount)
	}
	return TransferResult{TxHash: hash}, nil
}

func (f *fakeClient) SubmitSweep(ctx context.Context, req SweepRequest) (SweepResult, error) {
	_ = ctx
	if req.MemberAddress == "" || req.TreasuryAddress == "" || req.Amount <= 0 {
		return SweepResult{}, fmt.Errorf("%w: invalid sweep", ErrAPI)
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
	f.lastSweep = req
	f.sweepCount++
	f.memberBalances[req.MemberAddress] -= req.Amount
	f.treasuryBalances[req.TreasuryAddress] += req.Amount
	hash := deterministicTxHash(req.MemberAddress, req.TreasuryAddress, req.Amount, f.sweepCount)
	return SweepResult{TxHash: hash}, nil
}

func (f *fakeClient) SendTreasuryTransaction(ctx context.Context, treasury TreasuryRef, to string, data []byte, valueWei *big.Int) (string, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.treasuryTxCount++
	f.lastTreasuryTx = treasuryTxRecord{treasury: treasury, to: to, data: data, valueWei: valueWei}
	hash := deterministicTxHash(treasury.Address, to, int64(len(data)), f.treasuryTxCount)
	return hash, nil
}

func SetMemberUSDCBalance(client Client, address string, amount int64) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: SetMemberUSDCBalance requires NewFakeClient")
	}
	f.mu.Lock()
	f.memberBalances[address] = amount
	f.mu.Unlock()
}

func SetTreasuryUSDCBalance(client Client, address string, amount int64) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: SetTreasuryUSDCBalance requires NewFakeClient")
	}
	f.mu.Lock()
	f.treasuryBalances[address] = amount
	f.mu.Unlock()
}

func SetRejectSubmitSweep(client Client, reject bool, err error) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: SetRejectSubmitSweep requires NewFakeClient")
	}
	f.mu.Lock()
	f.rejectSubmitSweep = reject
	f.rejectSubmitSweepErr = err
	f.mu.Unlock()
}

func SetForcedTransferHash(client Client, hash string) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: SetForcedTransferHash requires NewFakeClient")
	}
	f.mu.Lock()
	f.forcedTransferHash = hash
	f.mu.Unlock()
}

func SetRejectSubmitTransfer(client Client, reject bool, err error) {
	f, ok := client.(*fakeClient)
	if !ok {
		panic("wallets: SetRejectSubmitTransfer requires NewFakeClient")
	}
	f.mu.Lock()
	f.rejectSubmitTransfer = reject
	f.rejectSubmitTransferErr = err
	f.mu.Unlock()
}

func LastPayUSDCRequest(client Client) (PayUSDCRequest, bool) {
	f, ok := client.(*fakeClient)
	if !ok {
		return PayUSDCRequest{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.payoutCount == 0 {
		return PayUSDCRequest{}, false
	}
	return f.lastPayout, true
}

func LastTransferRequest(client Client) (TransferRequest, bool) {
	f, ok := client.(*fakeClient)
	if !ok {
		return TransferRequest{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transferCount == 0 {
		return TransferRequest{}, false
	}
	return f.lastTransfer, true
}

func LastSweepRequest(client Client) (SweepRequest, bool) {
	f, ok := client.(*fakeClient)
	if !ok {
		return SweepRequest{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sweepCount == 0 {
		return SweepRequest{}, false
	}
	return f.lastSweep, true
}

func deterministicTxHash(from, to string, amount int64, count int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("tx:%s:%s:%d:%d", from, to, amount, count)))
	return "0x" + hex.EncodeToString(sum[:20])
}

func deterministicWalletID(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id))
	return "wallet-" + hex.EncodeToString(sum[:8])
}

func deterministicEVMAddress(scope, id string) string {
	sum := sha256.Sum256([]byte(scope + ":" + id + ":evm"))
	return "0x" + hex.EncodeToString(sum[:20])
}
