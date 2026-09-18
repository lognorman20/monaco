package app

import (
	"context"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestJupiterCatalogRoutabilityProber_reportsRoutableQuote(t *testing.T) {
	t.Parallel()

	client := jupiter.NewFakeClient()
	jupiter.RegisterQuoteBuy(client, "MintAAPL", CatalogRoutabilityProbeMicros, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: "MintAAPL",
	})

	prober := NewJupiterCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		SolanaMint: "MintAAPL",
	})
	if !routable {
		t.Fatal("expected routable=true for configured Jupiter quote")
	}
}

func TestJupiterCatalogRoutabilityProber_reportsNoRoute(t *testing.T) {
	t.Parallel()

	client := jupiter.NewFakeClient()
	prober := NewJupiterCatalogRoutabilityProber(client)
	routable := prober.IsRoutable(context.Background(), xstocks.CatalogAsset{
		Symbol:     "DEADx",
		SolanaMint: "MintDead",
	})
	if routable {
		t.Fatal("expected routable=false when Jupiter has no quote")
	}
}
