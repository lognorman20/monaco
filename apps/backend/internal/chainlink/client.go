package chainlink

import (
	"context"
	"errors"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/evm"
	"github.com/monaco/monaco/apps/backend/internal/marks"
)

// ErrNotConfigured means the live Chainlink client is not wired yet.
var ErrNotConfigured = errors.New("chainlink marks client not configured")

type liveClient struct{}

// NewClient is a stub until M6-T6.
func NewClient(chain evm.Client, catalog b20.Catalog) marks.Client {
	_ = chain
	_ = catalog
	return &liveClient{}
}

func (l *liveClient) USDCOnlyPot(ctx context.Context, treasury marks.TreasuryRef) (marks.NavInput, error) {
	_ = ctx
	_ = treasury
	return marks.NavInput{}, ErrNotConfigured
}

func (l *liveClient) MarkedPot(ctx context.Context, treasury marks.TreasuryRef, holdings []marks.CostBasis) (marks.NavInput, error) {
	_ = ctx
	_ = treasury
	_ = holdings
	return marks.NavInput{}, ErrNotConfigured
}
