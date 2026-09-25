package main

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const tSpaceXMint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"

type fakeMintLookup struct {
	asset xstocks.CatalogAsset
	ok    bool
}

func (f fakeMintLookup) LookupByMint(context.Context, string) (xstocks.CatalogAsset, bool, error) {
	return f.asset, f.ok, nil
}

type fakePrices struct {
	row jupiter.TokenPrice
}

func (f fakePrices) Prices(context.Context, []string) (map[string]jupiter.TokenPrice, error) {
	return map[string]jupiter.TokenPrice{tSpaceXMint: f.row}, nil
}

func TestSweepRun_preIpoBalance_usesNineDecimalsAndSkipsDust(t *testing.T) {
	lookup := fakeMintLookup{ok: true, asset: xstocks.CatalogAsset{
		Symbol:     "tSpaceX",
		SolanaMint: tSpaceXMint,
		Kind:       xstocks.AssetKindPreIPO,
		Decimals:   9,
	}}
	prices := fakePrices{row: jupiter.TokenPrice{PriceUsdcMicros: 100_000_000, Decimals: 9}}

	held, err := resolveSweepSell(context.Background(), lookup, prices, tSpaceXMint, 1_500_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if held.Decimals != 9 || held.SlippageBps != jupiter.PreIPOSlippageBps || held.WholeTokens != "1.5" || held.SkipDust {
		t.Fatalf("held plan: %+v", held)
	}

	dust, err := resolveSweepSell(context.Background(), lookup, prices, tSpaceXMint, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !dust.SkipDust || dust.WholeTokens != "0.001" || dust.Decimals != 9 {
		t.Fatalf("dust plan: %+v", dust)
	}
}

func TestSweepRun_unknownMint_usesPriceDecimalsThenEight(t *testing.T) {
	prices := fakePrices{row: jupiter.TokenPrice{PriceUsdcMicros: 1_000_000, Decimals: 9}}
	fromPrice, err := resolveSweepSell(context.Background(), fakeMintLookup{}, prices, tSpaceXMint, 2_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if fromPrice.Decimals != 9 || fromPrice.Kind != xstocks.AssetKindStock || fromPrice.SlippageBps != 0 {
		t.Fatalf("price fallback: %+v", fromPrice)
	}

	unknown, err := resolveSweepSell(context.Background(), fakeMintLookup{}, nil, "mint", 1)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Decimals != 8 || unknown.SkipDust {
		t.Fatalf("default plan: %+v", unknown)
	}
}
