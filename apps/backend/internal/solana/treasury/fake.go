package treasury

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	solanakey "github.com/monaco/monaco/apps/backend/internal/solana/key"
)

// Fake is a deterministic in-memory Solana treasury for tests. It never touches Privy or RPC.
type Fake struct {
	mu sync.Mutex

	balances       map[string]int64
	statuses       map[string]PayoutState
	defaultState   PayoutState
	inbound        []InboundTransfer
	inboundTo      map[string]string // signature -> treasury address
	prepareErr     error
	broadcastErr   error
	prepareCount   int
	broadcastCount int
	lastPayout     PayUSDCRequest
	unsettled      map[string]PayUSDCRequest // signature -> payout not yet debited
}

// NewFake returns a fake whose payouts confirm on the first status check.
func NewFake() *Fake {
	return &Fake{
		balances:     make(map[string]int64),
		statuses:     make(map[string]PayoutState),
		defaultState: PayoutStateConfirmed,
		inboundTo:    make(map[string]string),
		unsettled:    make(map[string]PayUSDCRequest),
	}
}

// FakeAddress derives a stable, valid base58 Solana address from label.
func FakeAddress(label string) string {
	sum := sha256.Sum256([]byte("monaco-fake-solana:" + label))
	return solanakey.EncodeBase58(sum[:])
}

func (f *Fake) EnsureTreasury(ctx context.Context, groupID string) (TreasuryRef, error) {
	_ = ctx
	if groupID == "" {
		return TreasuryRef{}, fmt.Errorf("%w: missing group id", ErrAPI)
	}
	return TreasuryRef{GroupID: groupID, PrivyWalletID: "fake-privy-" + groupID, SolanaAddress: FakeAddress("treasury-" + groupID)}, nil
}

func (f *Fake) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balances[treasuryAddress], nil
}

func (f *Fake) PrepareUSDCPayout(ctx context.Context, req PayUSDCRequest) (PreparedPayout, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prepareErr != nil {
		return PreparedPayout{}, f.prepareErr
	}
	if req.Amount <= 0 || req.ToAddress == "" || req.TreasuryRef.SolanaAddress == "" {
		return PreparedPayout{}, fmt.Errorf("%w: invalid payout request", ErrAPI)
	}
	f.prepareCount++
	f.lastPayout = req
	sig := FakeAddress(fmt.Sprintf("payout-%s-%s-%d", req.TreasuryRef.SolanaAddress, req.ToAddress, f.prepareCount)) +
		FakeAddress(fmt.Sprintf("payout-tail-%d", f.prepareCount))
	f.unsettled[sig] = req
	return PreparedPayout{TxSignature: sig, SignedTransaction: "signed-" + sig, LastValidBlockHeight: 1000}, nil
}

func (f *Fake) BroadcastUSDCPayout(ctx context.Context, payout PreparedPayout) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broadcastErr != nil {
		return f.broadcastErr
	}
	f.broadcastCount++
	return nil
}

func (f *Fake) USDCPayoutStatus(ctx context.Context, payout PreparedPayout) (PayoutStatus, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	state, ok := f.statuses[payout.TxSignature]
	if !ok {
		state = f.defaultState
	}
	// A confirmed payout has left the treasury; debit it the first time the chain says so.
	if req, pending := f.unsettled[payout.TxSignature]; pending && state == PayoutStateConfirmed {
		f.balances[req.TreasuryRef.SolanaAddress] -= req.Amount
		delete(f.unsettled, payout.TxSignature)
	}
	return PayoutStatus{State: state}, nil
}

func (f *Fake) ListInboundUSDCTransfers(ctx context.Context, q InboundQuery) ([]InboundTransfer, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []InboundTransfer
	for _, transfer := range f.inbound {
		if f.inboundTo[transfer.TxSignature] != q.TreasuryAddress || transfer.FromAddress != q.FromAddress {
			continue
		}
		if !q.Since.IsZero() && !transfer.BlockTime.IsZero() && transfer.BlockTime.Before(q.Since) {
			continue
		}
		out = append(out, transfer)
	}
	return out, nil
}

// SetUSDCBalance sets a treasury's SPL USDC balance.
func (f *Fake) SetUSDCBalance(address string, micros int64) {
	f.mu.Lock()
	f.balances[address] = micros
	f.mu.Unlock()
}

// SetDefaultPayoutState changes what status checks report for payouts without an override.
func (f *Fake) SetDefaultPayoutState(state PayoutState) {
	f.mu.Lock()
	f.defaultState = state
	f.mu.Unlock()
}

// SetPayoutState overrides the status of one payout signature.
func (f *Fake) SetPayoutState(signature string, state PayoutState) {
	f.mu.Lock()
	f.statuses[signature] = state
	f.mu.Unlock()
}

// FailPrepare makes PrepareUSDCPayout return err (nil clears it).
func (f *Fake) FailPrepare(err error) {
	f.mu.Lock()
	f.prepareErr = err
	f.mu.Unlock()
}

// FailBroadcast makes BroadcastUSDCPayout return err (nil clears it).
func (f *Fake) FailBroadcast(err error) {
	f.mu.Lock()
	f.broadcastErr = err
	f.mu.Unlock()
}

// AddInboundTransfer records a confirmed SPL USDC transfer into treasuryAddress.
func (f *Fake) AddInboundTransfer(treasuryAddress string, transfer InboundTransfer) {
	f.mu.Lock()
	f.inbound = append(f.inbound, transfer)
	f.inboundTo[transfer.TxSignature] = treasuryAddress
	f.mu.Unlock()
}

// PrepareCount is how many payouts were signed.
func (f *Fake) PrepareCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCount
}

// BroadcastCount is how many payouts were broadcast.
func (f *Fake) BroadcastCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.broadcastCount
}

// LastPayout is the most recent payout request.
func (f *Fake) LastPayout() PayUSDCRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPayout
}

var _ Client = (*Fake)(nil)
