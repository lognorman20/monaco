package app

import (
	"context"
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/wallets"
)

const execAAPL = "0xb200000000000000000000c2e324d24d7eecd1fb"

func setupExecBuy(t *testing.T, label string, chain evm.Client) (integrationHarness, string, string) {
	t.Helper()
	h := integrationApp(t)
	if chain != nil {
		h.Swap = NewSwapService(h.Store, NewBuyService(h.Jupiter, h.Catalog), h.Jupiter, h.Wallets, chain, h.Symbols)
	}
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	session := openTestSession(t, h.ISO, sessions, h.Auth, label, "Exec")
	gov := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	group, err := gov.CreateGroupWithRules(ctx, h.ISO.UniqueToken(label), testGroupName(h.ISO, label), DefaultGroupRules())
	if err != nil {
		t.Fatal(err)
	}
	h.ISO.TrackGroup(group.GroupID)
	const usdc int64 = 2_000_000
	registerHappyBuy(h.Jupiter, h.Catalog, execAAPL, usdc, "", "")
	treasury, err := h.Wallets.EnsureTreasury(ctx, wallets.GroupID(group.GroupID))
	if err != nil {
		t.Fatal(err)
	}
	h.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 5_000_000})
	wallets.SetTreasuryUSDCBalance(h.Wallets, treasury.Address, 5_000_000)
	return h, session.UserID, group.GroupID
}

func TestExecuteBuy_approvesWhenAllowanceShort_thenSwaps(t *testing.T) {
	chain := evm.NewFakeClient()
	h, userID, groupID := setupExecBuy(t, "exec-approve", chain)
	ctx := context.Background()
	treasury, _ := h.Wallets.EnsureTreasury(ctx, wallets.GroupID(groupID))
	chain.SetAllowance(dex.USDCAddress(), treasury.Address, "0xrouter", big.NewInt(0))
	wallets.SetNextTreasuryTxHashes(h.Wallets, "0xapprovehash0000000000000000000000000001", "0xswaphash00000000000000000000000000000002")
	chain.SetReceipt("0xapprovehash0000000000000000000000000001", evm.Receipt{Status: 1})
	chain.SetReceipt("0xswaphash00000000000000000000000000000002", evm.Receipt{Status: 1})
	if _, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{GroupID: groupID, UserID: userID, Symbol: "AAPLc", USDCAmount: 2_000_000}); err != nil {
		t.Fatal(err)
	}
	if wallets.TreasuryTxCount(h.Wallets) != 2 {
		t.Fatalf("txs = %d, want 2", wallets.TreasuryTxCount(h.Wallets))
	}
}

func TestExecuteBuy_skipsApproveWhenAllowanceSufficient(t *testing.T) {
	chain := evm.NewFakeClient()
	h, userID, groupID := setupExecBuy(t, "exec-skip", chain)
	ctx := context.Background()
	treasury, _ := h.Wallets.EnsureTreasury(ctx, wallets.GroupID(groupID))
	chain.SetAllowance(dex.USDCAddress(), treasury.Address, "0xrouter", big.NewInt(2_000_000))
	if _, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{GroupID: groupID, UserID: userID, Symbol: "AAPLc", USDCAmount: 2_000_000}); err != nil {
		t.Fatal(err)
	}
	if wallets.TreasuryTxCount(h.Wallets) != 1 {
		t.Fatalf("txs = %d, want 1", wallets.TreasuryTxCount(h.Wallets))
	}
}

func TestExecuteBuy_failedReceipt_marksTransactionFailed(t *testing.T) {
	chain := evm.NewFakeClient()
	h, userID, groupID := setupExecBuy(t, "exec-fail", chain)
	ctx := context.Background()
	treasury, _ := h.Wallets.EnsureTreasury(ctx, wallets.GroupID(groupID))
	chain.SetAllowance(dex.USDCAddress(), treasury.Address, "0xrouter", big.NewInt(2_000_000))
	wallets.SetNextTreasuryTxHashes(h.Wallets, "0xfailhash00000000000000000000000000000001")
	chain.SetReceipt("0xfailhash00000000000000000000000000000001", evm.Receipt{Status: 0})
	if _, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{GroupID: groupID, UserID: userID, Symbol: "AAPLc", USDCAmount: 2_000_000}); err == nil {
		t.Fatal("expected failure")
	}
}

func TestExecuteBuy_duplicateTxHash_isIdempotent(t *testing.T) {
	h, userID, groupID := setupExecBuy(t, "exec-dup", nil)
	ctx := context.Background()
	wallets.SetNextTreasuryTxHashes(h.Wallets, "0xdupe000000000000000000000000000000000001", "0xdupe000000000000000000000000000000000001")
	first, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{GroupID: groupID, UserID: userID, Symbol: "AAPLc", USDCAmount: 2_000_000})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.Swap.DevExecuteBuy(ctx, DevExecuteBuyRequest{GroupID: groupID, UserID: userID, Symbol: "AAPLc", USDCAmount: 2_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created && first.Transaction.ID != second.Transaction.ID {
		t.Fatal("duplicate execute created a second confirmed row")
	}
}

func TestSellToUSDC_happyPath_reducesTokenAndIncreasesTreasuryUsdc(t *testing.T) {
	h := integrationApp(t)
	ctx := context.Background()
	sessions := NewSessionService(h.Store, h.Auth, h.Wallets)
	session := openTestSession(t, h.ISO, sessions, h.Auth, "exec-sell", "Exec")
	gov := NewGovernanceService(h.Store, h.Auth, h.Wallets)
	group, err := gov.CreateGroupWithRules(ctx, h.ISO.UniqueToken("exec-sell"), testGroupName(h.ISO, "exec-sell"), DefaultGroupRules())
	if err != nil {
		t.Fatal(err)
	}
	h.ISO.TrackGroup(group.GroupID)
	registerDexSellQuote(t, h.Jupiter, execAAPL, 1_000_000, 900_000)
	treasury, err := h.Wallets.EnsureTreasury(ctx, wallets.GroupID(group.GroupID))
	if err != nil {
		t.Fatal(err)
	}
	h.Swap.SetTreasuryBalances(treasury.Address, TreasuryBalances{USDC: 0, Token: 1_000_000})
	if _, err := h.Swap.SellToUSDC(ctx, SellToUSDCRequest{GroupID: group.GroupID, UserID: session.UserID, Symbol: "AAPLc", InputToken: execAAPL, Amount: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	got := h.Swap.TreasuryBalancesFor(treasury.Address)
	if got.USDC != 900_000 {
		t.Fatalf("usdc = %d, want 900000", got.USDC)
	}
}
