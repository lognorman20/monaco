package trading

import (
	"context"

	chainadapter "github.com/monaco/monaco/apps/backend/internal/modules/trading/adapters/chain"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/app"
	"github.com/monaco/monaco/apps/backend/internal/modules/trading/domain"
	"github.com/monaco/monaco/apps/backend/internal/platform/bus"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain"
	"github.com/monaco/monaco/apps/backend/internal/platform/chain/solana"
	"github.com/monaco/monaco/apps/backend/internal/platform/httpx"
	"github.com/monaco/monaco/apps/backend/internal/platform/ids"
	"github.com/monaco/monaco/apps/backend/internal/platform/module"
	"github.com/monaco/monaco/apps/backend/internal/platform/poller"
)

type (
	SwapView       = app.SwapView
	Source         = domain.Source
	SwapLayer      = app.SwapLayer
	SwapLayerDeps  = app.SwapLayerDeps
	SwapRequest    = app.SwapRequest
	TreasuryWallet = app.TreasuryWallet
	Venue          = app.Venue
	Signer         = app.Signer
)

type Queries interface {
	Swap(ctx context.Context, id ids.SwapID) (SwapView, error)
	SwapBySignature(ctx context.Context, sig chain.Signature) (SwapView, error)
	LatestBySource(ctx context.Context, src Source) (SwapView, bool, error)
	HasLiveSwap(ctx context.Context, src Source) (bool, error)
	OwnsSignature(ctx context.Context, sig chain.Signature) (bool, error)
}

type Module struct {
	deps module.Deps
}

func New(d module.Deps) *Module { return &Module{deps: d} }

func (*Module) Name() string { return "trading" }

func (*Module) Routes(*httpx.Routes) {}

func (*Module) Consumers() []bus.Consumer {
	return []bus.Consumer{}
}

func (m *Module) Pollers() []poller.Poller {
	return []poller.Poller{app.NewSwapSweeper(m.deps.UoW, m.deps.Pool, m.deps.Clock,
		chainadapter.NewReader(solana.New(m.deps.Config, m.deps.Clock)), m.deps.Bus)}
}

func (m *Module) Queries() app.Queries { return app.NewQueries(m.deps.Pool) }
