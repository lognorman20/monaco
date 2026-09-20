package chainlink

import (
	"context"
	"math/big"
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
	stale := 25 * time.Hour
	now := l.now()
	for _, h := range holdings {
		feed, err := l.catalog.Feed(ctx, h.Symbol)
		if err != nil {
			return marks.NavInput{}, err
		}
		round, err := l.chain.ChainlinkLatestRoundData(ctx, feed)
		if err != nil {
			return marks.NavInput{}, err
		}
		if round.Answer == nil || round.Answer.Sign() <= 0 {
			return marks.NavInput{}, marks.ErrMarkUnavailable
		}
		// price micros = answer * 1e6 / 1e8
		price := new(big.Int).Mul(round.Answer, big.NewInt(1_000_000))
		price.Div(price, big.NewInt(100_000_000))
		if !price.IsInt64() {
			return marks.NavInput{}, marks.ErrMarkUnavailable
		}
		priceMicros := price.Int64()
		value := h.Units * priceMicros / 100_000_000
		afterHours := now.Sub(round.UpdatedAt) > stale
		out.Holdings = append(out.Holdings, marks.MarkedHolding{
			Symbol:     h.Symbol,
			Token:      h.Token,
			Units:      h.Units,
			MarkUsdc:   value,
			CostBasis:  h.Amount,
			AfterHours: afterHours,
		})
		if afterHours {
			out.AfterHours = true
		}
	}
	return out, nil
}
