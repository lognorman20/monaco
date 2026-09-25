package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/catalog"
	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/solana/mintinfo"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestRedeemBuffer_preIpo_followsConstant(t *testing.T) {
	if !catalog.JupiterNetsTransferFee {
		t.Fatal("JupiterNetsTransferFee must stay true for this guard test")
	}

	const (
		holding    = 2_000_000
		shortfall  = 500_000
		stockValue = 2_000_000
	)

	catalogSearcher := xstocks.NewFakeCatalogSearcher()
	xstocks.RegisterCatalogAsset(catalogSearcher, xstocks.CatalogAsset{
		Symbol:         "SPACEX",
		SolanaMint:     "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh",
		Kind:           xstocks.AssetKindPreIPO,
		TransferFeeBps: 100,
	})
	symbols := NewSymbolResolver(catalogSearcher)
	reader := mintinfo.NewFakeReader(map[string]mintinfo.Info{
		"PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh": {TransferFeeBps: 100},
	})

	bufferBps := redeemSellSlippageBufferBps(context.Background(), symbols, reader, "PreANxuXjsy2pvisWWMNB6YaJNzr7681wJJr2rHsfTh")
	if bufferBps != jupiter.RedeemSellSlippageBufferBps {
		t.Fatalf("buffer bps = %d, want slippage-only %d while JupiterNetsTransferFee is true", bufferBps, jupiter.RedeemSellSlippageBufferBps)
	}

	got := jupiter.RedeemShortfallSellAmount(holding, shortfall, stockValue, bufferBps)
	if got != 505_001 {
		t.Fatalf("sell amount = %d, want 505_001 at 100 bps buffer only", got)
	}

	withFeeBuffer := jupiter.RedeemShortfallSellAmount(holding, shortfall, stockValue, bufferBps+100)
	if withFeeBuffer <= got {
		t.Fatalf("redeem must not add transfer fee to buffer while JupiterNetsTransferFee is true: %d vs %d", withFeeBuffer, got)
	}
}
