package demochain

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

type stubPrices map[string]int64

func (s stubPrices) Prices(ctx context.Context, mints []string) (map[string]jupiter.TokenPrice, error) {
	out := map[string]jupiter.TokenPrice{}
	for _, m := range mints {
		if p, ok := s[m]; ok {
			out[m] = jupiter.TokenPrice{PriceUsdcMicros: p}
		}
	}
	return out, nil
}

func TestAMemberStartsWithABalanceAndFundingMovesItToThePot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewClient(privy.NewFakeClient(), StartingBalance)
	got, err := c.MemberUSDCBalance(ctx, "member-a")
	if err != nil || got != StartingBalance {
		t.Fatalf("first read = %d, %v; want %d", got, err, StartingBalance)
	}
	if _, err := c.SubmitSweep(ctx, privy.SweepRequest{MemberAddress: "member-a", TreasuryAddress: "pot-1", Amount: 500_000_000, RelayerKey: "k"}); err != nil {
		t.Fatal(err)
	}
	member, _ := c.MemberUSDCBalance(ctx, "member-a")
	pot, _ := c.TreasuryUSDCBalance(ctx, "pot-1")
	if member != 500_000_000 || pot != 500_000_000 {
		t.Fatalf("after funding: member %d pot %d", member, pot)
	}
	// A second look does not re-seed.
	if again, _ := c.MemberUSDCBalance(ctx, "member-a"); again != 500_000_000 {
		t.Fatalf("re-seeded to %d", again)
	}
}

func TestABuyFillsAtTheLivePriceAndSpendsThePot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	chain := privy.NewFakeClient()
	privy.SetTreasuryUSDCBalance(chain, "pot-1", 1_000_000_000)
	const googl = "GOOGLx-mint"
	p := NewSwapProvider(stubPrices{googl: 180_000_000}, chain) // $180 a share
	req := swapprovider.Request{GroupID: "g1", Symbol: "GOOGLx", Side: swapprovider.SideBuy, InputMint: jupiter.USDCMint, OutputMint: googl, OutputDecimals: 8, Amount: 250_000_000, Wallet: swapprovider.Wallet{SolanaAddress: "pot-1"}}
	sub, err := p.SubmitBuy(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// $250 at $180 is 1.38888888 shares, in 1e8 atomics.
	if sub.QuotedOutputAmount != 138_888_888 {
		t.Fatalf("quoted %d shares atomics", sub.QuotedOutputAmount)
	}
	fill, err := p.AwaitFill(ctx, sub, swapprovider.PollConfig{})
	if err != nil || !fill.Confirmed || fill.OutputAmount != sub.QuotedOutputAmount || fill.Signature == "" {
		t.Fatalf("fill = %+v, %v", fill, err)
	}
	pot, _ := chain.TreasuryUSDCBalance(ctx, "pot-1")
	if pot != 750_000_000 {
		t.Fatalf("pot after buy = %d", pot)
	}
	delta, err := chain.TokenBalanceDelta(ctx, fill.Signature, "pot-1", googl)
	if err != nil || delta != fill.OutputAmount {
		t.Fatalf("token delta = %d, %v", delta, err)
	}
	// Selling it all back at the same price returns the same money.
	sell, err := p.SubmitSell(ctx, swapprovider.Request{GroupID: "g1", Symbol: "GOOGLx", Side: swapprovider.SideSell, InputMint: googl, OutputMint: jupiter.USDCMint, InputDecimals: 8, Amount: fill.OutputAmount, Wallet: swapprovider.Wallet{SolanaAddress: "pot-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AwaitFill(ctx, sell, swapprovider.PollConfig{}); err != nil {
		t.Fatal(err)
	}
	pot, _ = chain.TreasuryUSDCBalance(ctx, "pot-1")
	if pot < 999_999_000 || pot > 1_000_000_000 {
		t.Fatalf("pot after round trip = %d", pot)
	}
}

func TestABuyThePotCannotCoverDoesNotFill(t *testing.T) {
	t.Parallel()
	chain := privy.NewFakeClient()
	p := NewSwapProvider(stubPrices{"m": 1_000_000}, chain)
	sub, _ := p.SubmitBuy(context.Background(), swapprovider.Request{Side: swapprovider.SideBuy, OutputMint: "m", OutputDecimals: 8, Amount: 5_000_000, Wallet: swapprovider.Wallet{SolanaAddress: "empty-pot"}})
	if _, err := p.AwaitFill(context.Background(), sub, swapprovider.PollConfig{}); err == nil {
		t.Fatal("an empty pot must not fill a buy")
	}
}

func TestTheFakeChainConfirmsEverything(t *testing.T) {
	t.Parallel()
	r := NewRPC()
	ok, _ := r.IsConfirmed(context.Background(), "DEMOabc")
	st, _ := r.SignatureStatus(context.Background(), "DEMOabc")
	h1, _ := r.FinalizedBlockHeight(context.Background())
	h2, _ := r.FinalizedBlockHeight(context.Background())
	if !ok || st.State != "confirmed" || h2 <= h1 {
		t.Fatalf("ok=%v state=%v heights=%d,%d", ok, st.State, h1, h2)
	}
}
