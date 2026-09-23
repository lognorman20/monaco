package chainlink

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/marks"
)

type liveClient struct {
	chain   evm.Client
	catalog b20.Catalog
	now     func() time.Time
}

// NewClient builds a marks.Client over Chainlink latestRoundData.
func NewClient(chain evm.Client, catalog b20.Catalog, now func() time.Time) marks.Client {
	if now == nil {
		now = time.Now
	}
	return &liveClient{chain: chain, catalog: catalog, now: now}
}

func (l *liveClient) USDCOnlyPot(ctx context.Context, treasury marks.TreasuryRef) (marks.NavInput, error) {
	if l.chain == nil {
		return marks.NavInput{TreasuryUsdc: treasury.TreasuryUsdc}, nil
	}
	bal, err := l.chain.ERC20Balance(ctx, evm.USDCAddress, treasury.Address)
	if err != nil {
		return marks.NavInput{}, err
	}
	if !bal.IsInt64() {
		return marks.NavInput{}, marks.ErrMarkUnavailable
	}
	return marks.NavInput{TreasuryUsdc: bal.Int64()}, nil
}

func (l *liveClient) MarkedPot(ctx context.Context, treasury marks.TreasuryRef, holdings []marks.CostBasis) (marks.NavInput, error) {
	usdc := treasury.TreasuryUsdc
	if l.chain != nil && treasury.Address != "" {
		bal, err := l.chain.ERC20Balance(ctx, evm.USDCAddress, treasury.Address)
		if err == nil && bal.IsInt64() {
			usdc = bal.Int64()
		}
	}
	out := marks.NavInput{TreasuryUsdc: usdc, Holdings: make([]marks.MarkedHolding, 0, len(holdings))}
	for _, h := range holdings {
		price, afterHours, err := l.markSymbol(ctx, h.Symbol)
		if err != nil {
			return marks.NavInput{}, err
		}
		out.Holdings = append(out.Holdings, marks.MarkedHolding{
			Symbol:     h.Symbol,
			Token:      h.Token,
			Units:      h.Units,
			MarkUsdc:   price,
			CostBasis:  h.Amount,
			AfterHours: afterHours,
		})
		if afterHours {
			out.AfterHours = true
		}
	}
	return out, nil
}

// roundMark is one symbol's latest round as a mark: the price, when the round
// was struck, and whether that is older than the feed's heartbeat.
type roundMark struct {
	price      int64
	updatedAt  time.Time
	afterHours bool
}

func (l *liveClient) markSymbol(ctx context.Context, symbol string) (int64, bool, error) {
	mark, err := l.markRoundSymbol(ctx, symbol)
	if err != nil {
		return 0, false, err
	}
	return mark.price, mark.afterHours, nil
}

func (l *liveClient) markRoundSymbol(ctx context.Context, symbol string) (roundMark, error) {
	if l.catalog == nil || l.chain == nil {
		return roundMark{}, marks.ErrMarkUnavailable
	}
	feed, err := l.catalog.Feed(ctx, symbol)
	if err != nil {
		return roundMark{}, err
	}
	round, err := l.chain.ChainlinkLatestRoundData(ctx, feed)
	if err != nil {
		return roundMark{}, err
	}
	return roundToRoundMark(l.now(), round)
}

func (l *liveClient) markSymbols(ctx context.Context, symbols []string) map[string]int64 {
	rounds := l.markRoundSymbols(ctx, symbols)
	out := make(map[string]int64, len(rounds))
	for symbol, mark := range rounds {
		out[symbol] = mark.price
	}
	return out
}

func (l *liveClient) markRoundSymbols(ctx context.Context, symbols []string) map[string]roundMark {
	out := make(map[string]roundMark, len(symbols))
	if l.catalog == nil || l.chain == nil {
		return out
	}
	feeds := make([]string, 0, len(symbols))
	symByFeed := make(map[string][]string, len(symbols))
	for _, symbol := range symbols {
		feed, err := l.catalog.Feed(ctx, symbol)
		if err != nil || strings.TrimSpace(feed) == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(feed))
		if _, seen := symByFeed[key]; !seen {
			feeds = append(feeds, key)
		}
		symByFeed[key] = append(symByFeed[key], symbol)
	}
	rounds, err := l.chain.ChainlinkLatestRoundDataMany(ctx, feeds)
	if err != nil {
		return out
	}
	now := l.now()
	for feed, round := range rounds {
		mark, convErr := roundToRoundMark(now, round)
		if convErr != nil {
			continue
		}
		for _, symbol := range symByFeed[strings.ToLower(feed)] {
			out[symbol] = mark
		}
	}
	return out
}

func roundToRoundMark(now time.Time, round evm.RoundData) (roundMark, error) {
	price, afterHours, err := roundToMark(now, round)
	if err != nil {
		return roundMark{}, err
	}
	return roundMark{price: price, updatedAt: round.UpdatedAt.UTC(), afterHours: afterHours}, nil
}

func roundToMark(now time.Time, round evm.RoundData) (int64, bool, error) {
	if round.Answer == nil || round.Answer.Sign() <= 0 {
		return 0, false, marks.ErrMarkUnavailable
	}
	price := new(big.Int).Mul(round.Answer, big.NewInt(1_000_000))
	price.Div(price, big.NewInt(100_000_000))
	if !price.IsInt64() {
		return 0, false, marks.ErrMarkUnavailable
	}
	stale := 25 * time.Hour
	afterHours := now.Sub(round.UpdatedAt) > stale
	return price.Int64(), afterHours, nil
}
