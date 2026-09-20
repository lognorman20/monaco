package faker

import (
	"context"

	"github.com/monaco/monaco/apps/backend/internal/marks"
)

// MarksSource adapts a marks client into a MarkSource for seeded cost bases.
func MarksSource(client marks.Client) MarkSource {
	if client == nil {
		return nil
	}
	return func(ctx context.Context, symbol, token string) (int64, bool) {
		input, err := client.MarkedPot(ctx, marks.TreasuryRef{}, []marks.CostBasis{{Symbol: symbol, Token: token, Units: 1_000_000, Price: 1, Amount: 1}})
		if err != nil || len(input.Holdings) == 0 || input.Holdings[0].MarkUsdc <= 0 {
			return 0, false
		}
		return input.Holdings[0].MarkUsdc, true
	}
}
