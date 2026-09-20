package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/dex"
	"github.com/monaco/monaco/apps/backend/internal/b20"
)

func TestJupiterCatalogRoutabilityProber_reportsRoutableQuote(t *testing.T) {
	t.Parallel()

	client := dex.NewFakeClient()
	jupiter.RegisterQuoteBuy(client, "MintAAPL", CatalogRoutabilityProbeMicros, jupiter.BuyQuote{
		Routable:   true,
		InputToken:  evm.USDCAddress,
		OutputToken: "MintAAPL",
	})

	prober := NewJupiterCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), b20.Asset{
		Symbol:     "AAPLx",
		TokenAddress: "MintAAPL",
	})
	if !routable {
		t.Fatal("expected routable=true for configured Jupiter quote")
	}
}

func TestJupiterCatalogRoutabilityProber_reportsNoRoute(t *testing.T) {
	t.Parallel()

	client := dex.NewFakeClient()
	prober := NewJupiterCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), b20.Asset{
		Symbol:     "DEADx",
		TokenAddress: "MintDead",
	})
	if routable {
		t.Fatal("expected routable=false when Jupiter has no quote")
	}
}
