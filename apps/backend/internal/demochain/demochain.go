// Package demochain is DEMO_MODE: real sign-in and real wallets, fake money.
//
// Every member starts with a balance, funding a cabal moves it into the pot, a passed
// buy fills at Jupiter's live price the moment it passes, a sale and a cash-out land
// at once. Nothing touches Solana. It exists so a walkthrough with three accounts can
// be recorded end to end, and so a first run of the app has something to do before
// anyone sends USDC. It is off unless DEMO_MODE=1 and it says so at startup.
package demochain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/worker"
)

// StartingBalance is what every member's account balance reads the first time it is
// looked at: a thousand dollars, enough to fund a cabal on camera a few times over.
const StartingBalance int64 = 1_000_000_000

// Client answers sign-in and wallet questions from the real Privy client and every
// chain question from an in-memory ledger.
type Client struct {
	auth  privy.Client
	chain privy.Client

	mu       sync.Mutex
	seeded   map[string]bool
	starting int64
}

// NewClient wraps the real client. `starting` is the balance a member is seeded with.
func NewClient(auth privy.Client, starting int64) *Client {
	return &Client{auth: auth, chain: privy.NewFakeClient(), seeded: make(map[string]bool), starting: starting}
}

// Chain is the ledger behind the client, for the swap provider that moves money on fills.
func (c *Client) Chain() privy.Client { return c.chain }

func (c *Client) VerifySession(ctx context.Context, token privy.AccessToken) (privy.Identity, error) {
	return c.auth.VerifySession(ctx, token)
}

func (c *Client) EnsureMemberWallet(ctx context.Context, privyUserID string, userID privy.UserID) (privy.WalletRef, error) {
	return c.auth.EnsureMemberWallet(ctx, privyUserID, userID)
}

func (c *Client) EnsureTreasury(ctx context.Context, groupID privy.GroupID) (privy.TreasuryRef, error) {
	return c.auth.EnsureTreasury(ctx, groupID)
}

// VerifyPayoutProof is pure signature checking against the member's wallet, so the
// real client does it; the fake would only accept its own scripted proofs.
func (c *Client) VerifyPayoutProof(ctx context.Context, userID string, proof privy.PayoutProof) error {
	return c.auth.VerifyPayoutProof(ctx, userID, proof)
}

// MemberUSDCBalance seeds a member the first time they are looked at.
func (c *Client) MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error) {
	c.mu.Lock()
	if !c.seeded[memberAddress] {
		c.seeded[memberAddress] = true
		privy.SetMemberUSDCBalance(c.chain, memberAddress, c.starting)
	}
	c.mu.Unlock()
	return c.chain.MemberUSDCBalance(ctx, memberAddress)
}

func (c *Client) TreasuryUSDCBalance(ctx context.Context, treasuryAddress string) (int64, error) {
	return c.chain.TreasuryUSDCBalance(ctx, treasuryAddress)
}

func (c *Client) SubmitSweep(ctx context.Context, req privy.SweepRequest) (privy.SweepResult, error) {
	return c.chain.SubmitSweep(ctx, req)
}

// sweepBroadcaster is the two-step sweep the poller uses; the fake has it, the
// Client interface does not name it.
type sweepBroadcaster interface {
	PrepareSweep(ctx context.Context, req privy.SweepRequest) (privy.PreparedSweep, error)
	BroadcastSweep(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, error)
}

func (c *Client) PrepareSweep(ctx context.Context, req privy.SweepRequest) (privy.PreparedSweep, error) {
	return c.chain.(sweepBroadcaster).PrepareSweep(ctx, req)
}

func (c *Client) BroadcastSweep(ctx context.Context, prepared privy.PreparedSweep) (privy.SweepResult, error) {
	return c.chain.(sweepBroadcaster).BroadcastSweep(ctx, prepared)
}

func (c *Client) SubmitMemberUSDCTransfer(ctx context.Context, req privy.TransferRequest) (privy.TransferResult, error) {
	return c.chain.SubmitMemberUSDCTransfer(ctx, req)
}

func (c *Client) PrepareUSDCPayout(ctx context.Context, req privy.PayUSDCRequest) (privy.PreparedPayout, error) {
	return c.chain.PrepareUSDCPayout(ctx, req)
}

func (c *Client) BroadcastUSDCPayout(ctx context.Context, payout privy.PreparedPayout) error {
	return c.chain.BroadcastUSDCPayout(ctx, payout)
}

func (c *Client) USDCPayoutStatus(ctx context.Context, payout privy.PreparedPayout) (privy.PayoutStatus, error) {
	return c.chain.USDCPayoutStatus(ctx, payout)
}

func (c *Client) TokenBalanceDelta(ctx context.Context, signature, owner, mint string) (int64, error) {
	return c.chain.TokenBalanceDelta(ctx, signature, owner, mint)
}

// RPC confirms every signature it is asked about. Demo fills and sweeps are all
// "on chain" the moment they are made.
type RPC struct {
	height atomic.Uint64
}

// NewRPC starts the fake chain at a plausible height.
func NewRPC() *RPC {
	r := &RPC{}
	r.height.Store(300_000_000)
	return r
}

func (r *RPC) SignatureStatus(ctx context.Context, txSignature string) (worker.SignatureStatus, error) {
	if txSignature == "" {
		return worker.SignatureStatus{State: worker.SignatureNotFound}, nil
	}
	return worker.SignatureStatus{State: worker.SignatureConfirmed}, nil
}

func (r *RPC) FinalizedBlockHeight(ctx context.Context) (uint64, error) {
	return r.height.Add(1), nil
}

// IsConfirmed satisfies the platform withdrawal's confirmer.
func (r *RPC) IsConfirmed(ctx context.Context, txSignature string) (bool, error) {
	return txSignature != "", nil
}

// SwapProvider fills a treasury swap at Jupiter's live price, instantly, and moves
// the pot's USDC on the ledger the way a real fill would.
type SwapProvider struct {
	prices jupiter.PriceClient
	chain  privy.Client
	count  atomic.Int64
}

// NewSwapProvider prices with the real (free) Jupiter price API and settles on the
// demo ledger.
func NewSwapProvider(prices jupiter.PriceClient, chain privy.Client) *SwapProvider {
	return &SwapProvider{prices: prices, chain: chain}
}

func (p *SwapProvider) Name() string { return "demo" }

func (p *SwapProvider) SubmitBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	price, err := p.priceMicros(ctx, req.OutputMint)
	if err != nil {
		return swapprovider.Submission{}, err
	}
	// tokens = usdc / price, in the token's own atomics.
	out := scale(req.Amount, req.OutputDecimals, price)
	if out <= 0 {
		return swapprovider.Submission{}, fmt.Errorf("demo swap: %s prices to nothing", req.Symbol)
	}
	return swapprovider.Submission{
		RequestID:          p.requestID(req),
		Request:            req,
		QuotedOutputAmount: out,
		QuotedInputAmount:  req.Amount,
	}, nil
}

func (p *SwapProvider) SubmitSell(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	price, err := p.priceMicros(ctx, req.InputMint)
	if err != nil {
		return swapprovider.Submission{}, err
	}
	// usdc = tokens * price, from the token's atomics.
	out := unscale(req.Amount, req.InputDecimals, price)
	if out <= 0 {
		return swapprovider.Submission{}, fmt.Errorf("demo swap: %s sells for nothing", req.Symbol)
	}
	return swapprovider.Submission{
		RequestID:          p.requestID(req),
		Request:            req,
		QuotedOutputAmount: out,
		QuotedInputAmount:  req.Amount,
	}, nil
}

// AwaitFill settles at the quoted amounts and moves the pot's USDC.
func (p *SwapProvider) AwaitFill(ctx context.Context, sub swapprovider.Submission, cfg swapprovider.PollConfig) (swapprovider.Fill, error) {
	req := sub.Request
	sig := "DEMO" + hexSum("fill:"+sub.RequestID)
	treasury := req.Wallet.SolanaAddress
	balance, err := p.chain.TreasuryUSDCBalance(ctx, treasury)
	if err != nil {
		return swapprovider.Fill{}, err
	}
	switch req.Side {
	case swapprovider.SideSell:
		privy.SetTreasuryUSDCBalance(p.chain, treasury, balance+sub.QuotedOutputAmount)
		privy.RegisterTokenBalanceDelta(p.chain, sig, treasury, req.InputMint, -sub.QuotedInputAmount)
		privy.RegisterTokenBalanceDelta(p.chain, sig, treasury, jupiter.USDCMint, sub.QuotedOutputAmount)
	default:
		if balance < sub.QuotedInputAmount {
			return swapprovider.Fill{Confirmed: false, Status: "Failed", Code: 1}, fmt.Errorf("demo swap: the pot holds %d, the buy needs %d", balance, sub.QuotedInputAmount)
		}
		privy.SetTreasuryUSDCBalance(p.chain, treasury, balance-sub.QuotedInputAmount)
		privy.RegisterTokenBalanceDelta(p.chain, sig, treasury, req.OutputMint, sub.QuotedOutputAmount)
		privy.RegisterTokenBalanceDelta(p.chain, sig, treasury, jupiter.USDCMint, -sub.QuotedInputAmount)
	}
	slog.Info("demo swap filled", "side", req.Side, "symbol", req.Symbol, "in", sub.QuotedInputAmount, "out", sub.QuotedOutputAmount, "tx_signature", sig)
	return swapprovider.Fill{
		Confirmed:          true,
		Signature:          sig,
		Status:             "Success",
		InputAmount:        sub.QuotedInputAmount,
		OutputAmount:       sub.QuotedOutputAmount,
		QuotedOutputAmount: sub.QuotedOutputAmount,
		QuotedInputAmount:  sub.QuotedInputAmount,
	}, nil
}

func (p *SwapProvider) priceMicros(ctx context.Context, mint string) (int64, error) {
	prices, err := p.prices.Prices(ctx, []string{mint})
	if err != nil {
		return 0, fmt.Errorf("demo swap: price %s: %w", mint, err)
	}
	price, ok := prices[mint]
	if !ok || price.PriceUsdcMicros <= 0 {
		return 0, fmt.Errorf("demo swap: no price for %s", mint)
	}
	return price.PriceUsdcMicros, nil
}

func (p *SwapProvider) requestID(req swapprovider.Request) string {
	n := p.count.Add(1)
	return "demo-" + hexSum(fmt.Sprintf("%s:%s:%s:%d:%d:%d", req.GroupID, req.Symbol, req.Side, req.Amount, time.Now().UnixNano(), n))[:24]
}

// scale turns USDC micros into token atomics at a price in USDC micros per whole token.
func scale(usdcMicros int64, decimals int, priceMicros int64) int64 {
	num := new(big.Int).Mul(big.NewInt(usdcMicros), pow10(decimals))
	out := new(big.Int).Quo(num, big.NewInt(priceMicros))
	if !out.IsInt64() {
		return math.MaxInt64
	}
	return out.Int64()
}

// unscale turns token atomics into USDC micros at the same price.
func unscale(atomics int64, decimals int, priceMicros int64) int64 {
	num := new(big.Int).Mul(big.NewInt(atomics), big.NewInt(priceMicros))
	out := new(big.Int).Quo(num, pow10(decimals))
	if !out.IsInt64() {
		return math.MaxInt64
	}
	return out.Int64()
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func hexSum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:20])
}
