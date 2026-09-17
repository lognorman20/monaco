package pyth

import "context"

// Client fetches USDC-only and marked pot inputs from Pyth Hermes.
type Client interface {
	USDCOnlyPot(ctx context.Context, treasury TreasuryRef) (NavInput, error)
	MarkedPot(ctx context.Context, treasury TreasuryRef, holdings []CostBasis) (NavInput, error)
}
