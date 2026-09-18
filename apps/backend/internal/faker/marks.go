package faker

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

// PythMarkSource adapts a Pyth client into a MarkSource (whole-share USDC micros) so seeded
// cost bases sit near live marks. Returns nil when client is nil; the seeder then uses fixed
// reference marks. Pyth Hermes is a price feed only: no Privy, RPC, or Jupiter involvement.
func PythMarkSource(client pyth.Client) MarkSource {
	if client == nil {
		return nil
	}
	return func(ctx context.Context, symbol, mint string) (int64, bool) {
		input, err := client.MarkedPot(ctx, pyth.TreasuryRef{}, []pyth.CostBasis{{Symbol: symbol, Mint: mint, Units: 1_000_000, Price: 1, Amount: 1}})
		if err != nil || len(input.Holdings) == 0 || input.Holdings[0].MarkUsdc <= 0 {
			return 0, false
		}
		return input.Holdings[0].MarkUsdc, true
	}
}
