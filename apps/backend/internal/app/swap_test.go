package app

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const testPreIPOMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"

type recordingSwapProvider struct {
	lastReq swapprovider.Request
}

func (p *recordingSwapProvider) Name() string { return "recording" }

func (p *recordingSwapProvider) SubmitBuy(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	p.lastReq = req
	return swapprovider.Submission{RequestID: "rec-buy-1", QuotedOutputAmount: 100_000_000_000, Request: req}, nil
}

func (p *recordingSwapProvider) SubmitSell(ctx context.Context, req swapprovider.Request) (swapprovider.Submission, error) {
	p.lastReq = req
	return swapprovider.Submission{RequestID: "rec-sell-1", QuotedInputAmount: req.Amount, Request: req}, nil
}

func (p *recordingSwapProvider) AwaitFill(ctx context.Context, sub swapprovider.Submission, cfg swapprovider.PollConfig) (swapprovider.Fill, error) {
	if sub.Request.Side == swapprovider.SideBuy {
		return swapprovider.Fill{
			Confirmed:          true,
			Signature:          "sig-buy-rec",
			Status:             jupiter.ExecuteStatusSuccess,
			Code:               0,
			InputAmount:        sub.Request.Amount,
			OutputAmount:       100_000_000_000,
			QuotedOutputAmount: sub.QuotedOutputAmount,
		}, nil
	}
	return swapprovider.Fill{
		Confirmed:    true,
		Signature:    "sig-sell-rec",
		Status:       jupiter.ExecuteStatusSuccess,
		Code:         0,
		OutputAmount: 2_000_000,
	}, nil
}

func registerPreIPOCatalog(t *testing.T, h integrationHarness, symbol string) {
	t.Helper()
	xstocks.RegisterCatalogAsset(h.Catalog, xstocks.CatalogAsset{
		Symbol:         symbol,
		Name:           "T-SpaceX",
		SolanaMint:     testPreIPOMint,
		Kind:           xstocks.AssetKindPreIPO,
		Source:         xstocks.AssetSourceTessera,
		Decimals:       9,
		TransferFeeBps: 20,
	})
	xstocks.RegisterSolanaMint(h.XStocks, symbol, testPreIPOMint)
}

func TestDevExecuteBuy_preIpo_usesNineDecimalsAnd100bps(t *testing.T) {
	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")

	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "preipo-buy", "PreIPO Buyer")
	token := h.ISO.UniqueToken("preipo-buy")
	group, err := h.Groups.CreateGroup(context.Background(), string(token), testGroupName(h.ISO, "preipo-buy"))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	treasury, err := h.Privy.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("ensure treasury: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, treasury.SolanaAddress, 5_000_000)
	privy.RegisterTokenBalanceDelta(h.Privy, "sig-buy-rec", treasury.SolanaAddress, testPreIPOMint, 100_000_000_000)

	recorder := &recordingSwapProvider{}
	h.Swap.SetSwapProvider(recorder)

	_, err = h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "tSpaceX",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}
	if recorder.lastReq.OutputDecimals != 9 {
		t.Fatalf("OutputDecimals = %d, want 9", recorder.lastReq.OutputDecimals)
	}
	if recorder.lastReq.Kind != swapprovider.AssetKindPreIPO {
		t.Fatalf("Kind = %q, want pre_ipo", recorder.lastReq.Kind)
	}
	if got := jupiter.SlippageBpsForRequest(recorder.lastReq); got != 100 {
		t.Fatalf("slippageBps = %d, want 100", got)
	}

	xstocks.RegisterSolanaMint(h.XStocks, "AAPLx", jupiter.AAPLxMint)
	privy.RegisterTokenBalanceDelta(h.Privy, "sig-buy-rec", treasury.SolanaAddress, jupiter.AAPLxMint, 100_000_000)
	_, err = h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy stock: %v", err)
	}
	if recorder.lastReq.OutputDecimals != 8 {
		t.Fatalf("stock OutputDecimals = %d, want 8", recorder.lastReq.OutputDecimals)
	}
	if got := jupiter.SlippageBpsForRequest(recorder.lastReq); got != 50 {
		t.Fatalf("stock slippageBps = %d, want 50", got)
	}
}

func registerIntegrationBuyFill(t *testing.T, h integrationHarness, label, mint string, usdc, quotedOut int64) (requestID, signature string) {
	t.Helper()
	requestID = fmt.Sprintf("buy-%s-%d", label, usdc)
	signature = "sig-" + requestID
	jupiter.RegisterQuoteBuy(h.Jupiter, mint, usdc, jupiter.BuyQuote{
		Routable:   true,
		OutputMint: mint,
		InAmount:   strconv.FormatInt(usdc, 10),
		OutAmount:  strconv.FormatInt(quotedOut, 10),
		RequestID:  requestID,
	})
	jupiter.RegisterExecutePoll(h.Jupiter, requestID, []jupiter.ExecuteResult{{
		Status:             jupiter.ExecuteStatusSuccess,
		Code:               0,
		Signature:          signature,
		InputAmountResult:  strconv.FormatInt(usdc, 10),
		OutputAmountResult: strconv.FormatInt(quotedOut, 10),
	}})
	return requestID, signature
}

func TestConfirmBuy_persistsChainDeltaOverQuote(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")
	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "chain-delta", "Chain Delta")
	group, err := h.Groups.CreateGroup(context.Background(), string(h.ISO.UniqueToken("chain-delta")), testGroupName(h.ISO, "chain-delta"))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	treasury, err := h.Privy.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("ensure treasury: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, treasury.SolanaAddress, 5_000_000)

	const (
		usdc       = 2_000_000
		quotedOut  = 100_000_000_000
		receivedOut = 99_800_000_000
	)
	_, signature := registerIntegrationBuyFill(t, h, h.ISO.Suffix(), testPreIPOMint, usdc, quotedOut)
	privy.RegisterTokenBalanceDelta(h.Privy, signature, treasury.SolanaAddress, testPreIPOMint, receivedOut)

	result, err := h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "tSpaceX",
		USDCAmount: usdc,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}
	if result.Transaction.CostBasisAmount.Int64 != receivedOut {
		t.Fatalf("cost_basis_amount = %d, want %d", result.Transaction.CostBasisAmount.Int64, receivedOut)
	}
	if result.Transaction.TokenDecimals != 9 {
		t.Fatalf("token_decimals = %d, want 9", result.Transaction.TokenDecimals)
	}
	if !strings.Contains(logBuf.String(), `"fee_bps_observed":20`) {
		t.Fatalf("expected fee_bps_observed=20 in logs, got: %s", logBuf.String())
	}
}

func TestConfirmBuy_rpcUnavailable_fallsBackToJupiterOutput(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")
	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "jup-fallback", "Jupiter Fallback")
	group, err := h.Groups.CreateGroup(context.Background(), string(h.ISO.UniqueToken("jup-fallback")), testGroupName(h.ISO, "jup-fallback"))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	treasury, err := h.Privy.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("ensure treasury: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, treasury.SolanaAddress, 5_000_000)

	const (
		usdc      = 2_000_000
		jupiterOut = 100_000_000_000
	)
	_, signature := registerIntegrationBuyFill(t, h, h.ISO.Suffix()+"fb", testPreIPOMint, usdc, jupiterOut)
	privy.RegisterTokenBalanceDeltaError(h.Privy, signature, treasury.SolanaAddress, testPreIPOMint, fmt.Errorf("rpc down"))

	result, err := h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "tSpaceX",
		USDCAmount: usdc,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy: %v", err)
	}
	if result.Transaction.CostBasisAmount.Int64 != jupiterOut {
		t.Fatalf("cost_basis_amount = %d, want jupiter %d", result.Transaction.CostBasisAmount.Int64, jupiterOut)
	}
	if !strings.Contains(logBuf.String(), "fill reconciliation unavailable") {
		t.Fatalf("expected fill reconciliation unavailable warning, got: %s", logBuf.String())
	}
}

func TestSellToUSDC_preIpo_usesNineDecimals(t *testing.T) {
	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")
	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "preipo-sell", "PreIPO Seller")
	group, err := h.Groups.CreateGroup(context.Background(), string(h.ISO.UniqueToken("preipo-sell")), testGroupName(h.ISO, "preipo-sell"))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	h.ISO.TrackGroup(group.GroupID)
	treasury, err := h.Privy.EnsureTreasury(context.Background(), privy.GroupID(group.GroupID))
	if err != nil {
		t.Fatalf("ensure treasury: %v", err)
	}
	seedTestTreasuryUSDC(t, h.Privy, treasury.SolanaAddress, 5_000_000)

	recorder := &recordingSwapProvider{}
	h.Swap.SetSwapProvider(recorder)

	const sellAmount = 1_000_000_000
	_, err = h.Swap.SellToUSDC(context.Background(), SellToUSDCRequest{
		GroupID:   group.GroupID,
		UserID:    "user-1",
		Symbol:    "tSpaceX",
		InputMint: testPreIPOMint,
		Amount:    sellAmount,
	})
	if err != nil {
		t.Fatalf("SellToUSDC: %v", err)
	}
	if recorder.lastReq.InputDecimals != 9 {
		t.Fatalf("InputDecimals = %d, want 9", recorder.lastReq.InputDecimals)
	}
	if recorder.lastReq.Kind != swapprovider.AssetKindPreIPO {
		t.Fatalf("Kind = %q, want pre_ipo", recorder.lastReq.Kind)
	}
	_ = treasury
}
