package marks

import "context"

// Client fetches USDC-only and marked pot inputs.
type Client interface {
	USDCOnlyPot(ctx context.Context, treasury TreasuryRef) (NavInput, error)
	MarkedPot(ctx context.Context, treasury TreasuryRef, holdings []CostBasis) (NavInput, error)
}
