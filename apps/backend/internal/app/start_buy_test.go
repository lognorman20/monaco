package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func TestStartBuy_devRouteAllowed_whenDevFlagSet(t *testing.T) {
	// Arrange
	t.Setenv("DEV_BUY_ENABLED", "true")
	jupiterClient := jupiter.NewFakeClient()
	resolver := xstocks.NewFakeResolver()
	buy := NewBuyService(jupiterClient, resolver)
	xstocks.RegisterSolanaMint(resolver, "AAPLx", jupiter.AAPLxMint)
	jupiter.RegisterQuoteBuy(jupiterClient, jupiter.AAPLxMint, 1_000_000, jupiter.BuyQuote{
		Routable:   true,
		InputMint:  jupiter.USDCMint,
		OutputMint: jupiter.AAPLxMint,
		InAmount:   "1000000",
		OutAmount:  "500000",
	})

	// Act
	_, err := buy.StartBuyViaDevRoute(context.Background(), StartBuyRequest{
		GroupID:    "00000000-0000-0000-0000-000000000001",
		UserID:     "00000000-0000-0000-0000-000000000002",
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if err != nil {
		t.Fatalf("StartBuyViaDevRoute: %v", err)
	}
}

func TestStartBuy_devRouteBlocked_whenDevFlagUnset(t *testing.T) {
	// Arrange
	t.Setenv("DEV_BUY_ENABLED", "")
	jupiterClient := jupiter.NewFakeClient()
	resolver := xstocks.NewFakeResolver()
	buy := NewBuyService(jupiterClient, resolver)

	// Act
	_, err := buy.StartBuyViaDevRoute(context.Background(), StartBuyRequest{
		GroupID:    "00000000-0000-0000-0000-000000000001",
		UserID:     "00000000-0000-0000-0000-000000000002",
		Symbol:     "AAPLx",
		USDCAmount: 1_000_000,
	})

	// Assert
	if !errors.Is(err, ErrDevRouteBlocked) {
		t.Fatalf("error = %v, want ErrDevRouteBlocked", err)
	}
}
