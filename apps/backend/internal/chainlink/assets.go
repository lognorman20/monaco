package chainlink

import (
	"context"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

type assetPrices struct {
	live *liveClient
	// charts caches one resolved series per symbol and range, so the chart route,
	// the list's day change and the detail screen share one walk of the chain
	// instead of each paying for it.
	charts *chartCache
}

// NewAssetPrices returns catalog marks from Chainlink TRV feeds.
func NewAssetPrices(chain evm.Client, catalog b20.Catalog, now func() time.Time) pyth.AssetPriceClient {
	if now == nil {
		now = time.Now
	}
	return &assetPrices{
		live:   &liveClient{chain: chain, catalog: catalog, now: now},
		charts: newChartCache(now),
	}
}

// AssetMark and AssetMarks carry the round's updatedAt and the after-hours verdict
// along with the price. A total-return feed updates on deviation and on a daily
// heartbeat, so from Friday's close to Monday's open it holds Friday's price while
// the token's pools keep trading. Anything that compares the mark with a live
// price (the stock-vs-token premium) has to know how old it is.
func (a *assetPrices) AssetMark(ctx context.Context, symbol string) (pyth.AssetMark, error) {
	mark, err := a.live.markRoundSymbol(ctx, symbol)
	if err != nil {
		return pyth.AssetMark{}, err
	}
	return assetMarkFor(mark), nil
}

func (a *assetPrices) AssetMarks(ctx context.Context, symbols []string) (map[string]pyth.AssetMark, error) {
	marks := a.live.markRoundSymbols(ctx, symbols)
	out := make(map[string]pyth.AssetMark, len(marks))
	for symbol, mark := range marks {
		if mark.price <= 0 {
			continue
		}
		out[symbol] = assetMarkFor(mark)
	}
	return out, nil
}

func assetMarkFor(mark roundMark) pyth.AssetMark {
	return pyth.AssetMark{
		PriceUsdcMicros: mark.price,
		UpdatedAt:       mark.updatedAt,
		AfterHours:      mark.afterHours,
	}
}
