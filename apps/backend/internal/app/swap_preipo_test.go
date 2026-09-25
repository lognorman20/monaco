package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

func TestDevExecuteBuy_paused_refusesWithIssuerPaused(t *testing.T) {
	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")

	reader := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		testPreIPOMint: {Mint: testPreIPOMint, Paused: true, TransferFeeBps: 20},
	})
	h.Swap.SetMintInfo(reader)

	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "paused-buy", "Paused Buyer")
	group, err := h.Groups.CreateGroup(context.Background(), string(h.ISO.UniqueToken("paused-buy")), testGroupName(h.ISO, "paused-buy"))
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

	_, err = h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "tSpaceX",
		USDCAmount: 1_000_000,
		ProposalID: "prop-paused-1",
	})
	if !errors.Is(err, ErrIssuerPaused) {
		t.Fatalf("DevExecuteBuy err = %v, want ErrIssuerPaused", err)
	}
	if recorder.lastReq.Symbol != "" {
		t.Fatalf("swap provider invoked with %+v, want no submit", recorder.lastReq)
	}

	tx, found, err := h.Store.GetTransactionByExecuteRequestID(context.Background(), issuerPausedExecuteRequestID("prop-paused-1"))
	if err != nil {
		t.Fatalf("lookup failed tx: %v", err)
	}
	if !found {
		t.Fatal("expected failed transaction for issuer_paused execute")
	}
	if tx.Status != "failed" {
		t.Fatalf("transaction status = %q, want failed", tx.Status)
	}
}

func TestDevExecuteBuy_feeFromMintInfo_overridesStaticRow(t *testing.T) {
	h := integrationApp(t)
	registerPreIPOCatalog(t, h, "tSpaceX")

	reader := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		testPreIPOMint: {Mint: testPreIPOMint, TransferFeeBps: 100},
	})
	h.Swap.SetMintInfo(reader)

	sessions := NewSessionService(h.Store, h.Privy)
	openTestSession(t, h.ISO, sessions, h.Privy, "fee-buy", "Fee Buyer")
	group, err := h.Groups.CreateGroup(context.Background(), string(h.ISO.UniqueToken("fee-buy")), testGroupName(h.ISO, "fee-buy"))
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
	if recorder.lastReq.TransferFeeBps != 100 {
		t.Fatalf("TransferFeeBps = %d, want 100 from mintinfo", recorder.lastReq.TransferFeeBps)
	}
	if recorder.lastReq.Kind != swapprovider.AssetKindPreIPO {
		t.Fatalf("Kind = %q, want pre_ipo", recorder.lastReq.Kind)
	}

	// Static catalog row still says 20; mintinfo error must not zero the fee.
	h.Swap.SetMintInfo(mintinfo.NewFakeReader(map[string]mintinfo.Info{}))
	recorder.lastReq = swapprovider.Request{}
	_, err = h.Swap.DevExecuteBuy(context.Background(), DevExecuteBuyRequest{
		GroupID:    group.GroupID,
		UserID:     "user-1",
		Symbol:     "tSpaceX",
		USDCAmount: 1_000_000,
	})
	if err != nil {
		t.Fatalf("DevExecuteBuy fallback: %v", err)
	}
	if recorder.lastReq.TransferFeeBps != 20 {
		t.Fatalf("fallback TransferFeeBps = %d, want static catalog 20", recorder.lastReq.TransferFeeBps)
	}
}
