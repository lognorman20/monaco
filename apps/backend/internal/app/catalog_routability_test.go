package app

import (
	"context"
	"math/big"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/dex"
)

func TestDexCatalogRoutabilityProber_reportsRoutableQuote(t *testing.T) {
	t.Parallel()

	client := dex.NewFakeClient()
	dex.RegisterQuote(client, dex.Quote{
		TokenIn:   dex.USDCAddress(),
		TokenOut:  "MintAAPL",
		AmountIn:  big.NewInt(CatalogRoutabilityProbeMicros),
		AmountOut: big.NewInt(1),
		Routable:  true,
	})

	prober := NewDexCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), b20.Asset{
		Symbol:       "AAPLx",
		TokenAddress: "MintAAPL",
	})
	if !routable {
		t.Fatal("expected routable=true for configured DEX quote")
	}
}

func TestDexCatalogRoutabilityProber_reportsNoRoute(t *testing.T) {
	t.Parallel()

	client := dex.NewFakeClient()
	prober := NewDexCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), b20.Asset{
		Symbol:       "AAPLx",
		TokenAddress: "MintAAPL",
	})
	if routable {
		t.Fatal("expected routable=false without quote")
	}
}
