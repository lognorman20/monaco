package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// agentMarkConcurrency bounds parallel mark lookups so one assets page cannot fan out into a
// burst against the price source.
const agentMarkConcurrency = 4

// ErrAgentIntentNotFound is an intent id that is unknown or belongs to another agent.
var ErrAgentIntentNotFound = errors.New("agent intent not found")

// AgentAccount is what an agent sees about itself: its cabal, budget, cash and positions.
type AgentAccount struct {
	CabalName            string
	AgentName            string
	Status               domain.AgentStatus
	AllocationUsdcMicros int64
	AvailableUsdcMicros  int64
	// CashAvailableUsdcMicros is what a buy can spend now: the budget, capped by treasury USDC.
	CashAvailableUsdcMicros int64
	Holdings                []AgentHolding
}

// AgentHolding is one position the agent bought and may sell.
type AgentHolding struct {
	Symbol      string
	Name        string
	TokenAmount int64
	// MarkUsdcMicros and ValueUsdcMicros are nil when the stock has no mark right now.
	MarkUsdcMicros  *int64
	ValueUsdcMicros *int64
}

// AgentAsset is a tradable stock with its current mark, nil when unavailable.
type AgentAsset struct {
	xstocks.CatalogAsset
	MarkUsdcMicros *int64
}

// AgentAssetsPage is one page of tradable stocks.
type AgentAssetsPage struct {
	Assets  []AgentAsset
	HasMore bool
}

// AgentIntentView is one intent as its agent reads it back.
type AgentIntentView struct {
	ID                string
	Side              domain.AgentIntentSide
	Symbol            string
	Status            string
	RejectReason      string
	Reason            string
	IdempotencyKey    string
	UsdcMicros        *int64
	TokenAmount       *int64
	TransactionID     string
	TxSignature       string
	FilledTokenAmount *int64
	FilledUsdcMicros  *int64
	CreatedAt         time.Time
}

// WithMarketData gives the service the catalog and marks the agent's read routes price with.
func (s *AgentIntentService) WithMarketData(catalog xstocks.CatalogSearcher, marks pyth.AssetPriceClient) *AgentIntentService {
	s.catalog = catalog
	s.marks = marks
	return s
}

// Account reads the agent's cabal, budget, cash and own positions.
func (s *AgentIntentService) Account(ctx context.Context, agent postgres.GroupAgentRow) (AgentAccount, error) {
	if err := rejectFakerGroup(ctx, s.store, agent.GroupID); err != nil {
		return AgentAccount{}, err
	}
	group, found, err := s.store.GetGroupByID(ctx, agent.GroupID)
	if err != nil {
		return AgentAccount{}, err
	}
	if !found {
		return AgentAccount{}, ErrGroupNotFound
	}
	treasuryUSDC, err := s.treasuryUSDC(ctx, agent.GroupID)
	if err != nil {
		return AgentAccount{}, err
	}

	available, positions, err := s.readAgentLedger(ctx, groupAgentFromRow(agent))
	if err != nil {
		return AgentAccount{}, err
	}

	account := AgentAccount{
		CabalName:               group.Name,
		AgentName:               agent.AgentDisplayName,
		Status:                  agent.Status,
		AllocationUsdcMicros:    agent.AllocationUsdcMicros,
		AvailableUsdcMicros:     available,
		CashAvailableUsdcMicros: min(available, max(treasuryUSDC, 0)),
		Holdings:                make([]AgentHolding, 0, len(positions)),
	}
	symbols := make([]string, 0, len(positions))
	for _, position := range positions {
		holding := AgentHolding{TokenAmount: position.amount, Symbol: s.symbols.SymbolForMint(ctx, position.mint)}
		if s.catalog != nil {
			if asset, found, err := s.catalog.LookupByMint(ctx, position.mint); err == nil && found {
				holding.Symbol, holding.Name = asset.Symbol, asset.Name
			}
		}
		account.Holdings = append(account.Holdings, holding)
		symbols = append(symbols, holding.Symbol)
	}
	marks := s.fetchMarks(ctx, symbols)
	for i := range account.Holdings {
		holding := &account.Holdings[i]
		mark, ok := marks[holding.Symbol]
		if !ok {
			continue
		}
		value, err := domain.MulDivFloor(mark, holding.TokenAmount, jupiter.XStockAtomicScale)
		if err != nil {
			continue
		}
		holding.MarkUsdcMicros, holding.ValueUsdcMicros = &mark, &value
	}
	return account, nil
}

type agentPosition struct {
	mint   string
	amount int64
}

// readAgentLedger reads the agent's available budget and its sellable positions (only > 0).
func (s *AgentIntentService) readAgentLedger(ctx context.Context, agent domain.GroupAgent) (int64, []agentPosition, error) {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	spent, reserved, err := s.store.SumAgentBuyUSDCTx(ctx, tx, agent.ID)
	if err != nil {
		return 0, nil, err
	}
	proceeds, err := s.store.SumAgentSellProceedsUSDCTx(ctx, tx, agent.ID)
	if err != nil {
		return 0, nil, err
	}
	available, err := domain.AgentAvailableUsdcMicros(agent, domain.AgentTreasurySnapshot{
		AgentSpentUsdcMicros:        spent,
		PendingAgentUsdcMicros:      reserved,
		AgentSellProceedsUsdcMicros: proceeds,
	})
	if err != nil {
		return 0, nil, err
	}

	mints, err := s.store.ListAgentBoughtMintsTx(ctx, tx, agent.ID)
	if err != nil {
		return 0, nil, err
	}
	var positions []agentPosition
	for _, mint := range mints {
		amount, err := s.store.AgentSellableTokenAmountTx(ctx, tx, agent.ID, mint)
		if err != nil {
			return 0, nil, err
		}
		if amount > 0 {
			positions = append(positions, agentPosition{mint: mint, amount: amount})
		}
	}
	return available, positions, nil
}

// ListAssets returns routable catalog stocks with their marks. A stock whose mark fails keeps
// a nil mark instead of failing the page.
func (s *AgentIntentService) ListAssets(ctx context.Context, query string, limit, offset int) (AgentAssetsPage, error) {
	if s.catalog == nil {
		return AgentAssetsPage{}, errors.New("agent assets: no catalog configured")
	}
	page, err := s.catalog.Search(ctx, query, limit, offset)
	if err != nil {
		return AgentAssetsPage{}, err
	}
	routable := make([]xstocks.CatalogAsset, 0, len(page.Assets))
	symbols := make([]string, 0, len(page.Assets))
	for _, asset := range page.Assets {
		if asset.Routable {
			routable = append(routable, asset)
			symbols = append(symbols, asset.Symbol)
		}
	}
	marks := s.fetchMarks(ctx, symbols)
	out := AgentAssetsPage{Assets: make([]AgentAsset, 0, len(routable)), HasMore: page.HasMore}
	for _, asset := range routable {
		item := AgentAsset{CatalogAsset: asset}
		if mark, ok := marks[asset.Symbol]; ok {
			item.MarkUsdcMicros = &mark
		}
		out.Assets = append(out.Assets, item)
	}
	return out, nil
}

// fetchMarks returns a positive mark per symbol, leaving out every symbol whose mark failed.
func (s *AgentIntentService) fetchMarks(ctx context.Context, symbols []string) map[string]int64 {
	marks := make(map[string]int64, len(symbols))
	if s.marks == nil || len(symbols) == 0 {
		return marks
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, agentMarkConcurrency)
	for _, symbol := range symbols {
		if strings.TrimSpace(symbol) == "" {
			continue
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			mark, err := s.marks.AssetMark(ctx, symbol)
			if err != nil || mark.PriceUsdcMicros <= 0 {
				return
			}
			mu.Lock()
			marks[symbol] = mark.PriceUsdcMicros
			mu.Unlock()
		}()
	}
	wg.Wait()
	return marks
}

// GetIntent reads one of the agent's intents with its fill. The ledger decides the status: an
// intent whose swap confirmed reads executed even before its own row was updated.
func (s *AgentIntentService) GetIntent(ctx context.Context, agentID, intentID string) (AgentIntentView, error) {
	found, ok, err := s.store.GetAgentIntentByIDForAgent(ctx, agentID, intentID)
	if err != nil {
		return AgentIntentView{}, err
	}
	if !ok {
		return AgentIntentView{}, ErrAgentIntentNotFound
	}
	intent := found.Intent
	view := AgentIntentView{
		ID:                intent.ID,
		Side:              intent.Side,
		Symbol:            intent.Symbol,
		Status:            intent.Status,
		RejectReason:      intent.RejectReason.String,
		Reason:            intent.Reason.String,
		IdempotencyKey:    intent.IdempotencyKey.String,
		UsdcMicros:        optionalInt64(intent.UsdcMicros),
		TokenAmount:       optionalInt64(intent.TokenAmount),
		TransactionID:     found.TransactionID.String,
		TxSignature:       found.TxSignature.String,
		FilledTokenAmount: optionalInt64(found.FilledTokenAmount),
		FilledUsdcMicros:  optionalInt64(found.FilledUsdcMicros),
		CreatedAt:         intent.CreatedAt,
	}
	switch {
	case found.TransactionStatus.String == postgres.TransactionStatusConfirmed:
		view.Status, view.RejectReason = "executed", ""
	case found.TransactionStatus.String == postgres.TransactionStatusFailed && view.Status == "accepted":
		view.Status = "failed"
	}
	// A failure's stored reason is an internal error; the intent answer never exposed it.
	if view.Status == "failed" {
		view.RejectReason = agentIntentFailedReason
	}
	return view, nil
}

func optionalInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
