package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
	"github.com/monaco/monaco/packages/domain"
)

// PortfolioService answers "what do I own across my cabals" and "where did my money go".
//
// It owns no valuation of its own. Every figure is the member's slice of a pot valued by
// valuePot, the one valuation deposits, redeems, NAV snapshots and the cabal screens read,
// so the portfolio total is the same number Home shows as "Your money in cabals".
type PortfolioService struct {
	home *HomeService
}

// NewPortfolioService builds the portfolio and history reads on top of Home's dependencies.
func NewPortfolioService(home *HomeService) *PortfolioService {
	return &PortfolioService{home: home}
}

// PortfolioCabalLine is one cabal's part of a holding: the member's slice of that cabal's
// position.
type PortfolioCabalLine struct {
	GroupID    string
	Name       string
	Tint       string
	PictureURL string
	// ValueMicros is the member's slice of the cabal's position at the pot's mark.
	ValueMicros int64
	// Quantity is the member's slice of the cabal's tokens, as a decimal string in whole
	// shares or tokens.
	Quantity string
	// DollarPnLMicros is ValueMicros less the member's slice of the cabal's cost basis.
	DollarPnLMicros int64
	costMicros      int64
}

// PortfolioHolding is one stock across every cabal the member holds a slice of.
type PortfolioHolding struct {
	Symbol  string
	Name    string
	Kind    string
	LogoURL string
	// ValueMicros is the sum of the member's slices of this stock.
	ValueMicros int64
	// ShareOfTotal is ValueMicros over the portfolio total, 0..1.
	ShareOfTotal    string
	DollarPnLMicros int64
	// PercentReturn is nil when no cabal can say what its slice of the stock cost.
	PercentReturn *string
	Cabals        []PortfolioCabalLine
	costMicros    int64
}

// PortfolioResult is GET /v1/me/portfolio.
type PortfolioResult struct {
	// TotalMicros is the member's money in cabals: the sum of their slices of every pot.
	TotalMicros int64
	// CashMicros is the part of TotalMicros sitting in the pots as USDC.
	CashMicros int64
	// AccountBalanceMicros is USDC in the member's account, outside every cabal. Nil when
	// it could not be read: an unread balance is not an empty one.
	AccountBalanceMicros *int64
	DollarPnLMicros      int64
	PercentReturn        *string
	Holdings             []PortfolioHolding
	// UnvaluedCabals counts cabals the member holds a slice of that could not be valued on
	// this pass. They are left out of every figure above and counted here, so a short list
	// never reads as "you own nothing else".
	UnvaluedCabals int
}

// portfolioCabal is one joined cabal, valued, before holdings are merged across cabals.
type portfolioCabal struct {
	groupID    string
	name       string
	pictureURL string
	position   postgres.PositionRow
	valuation  potValuation
	valued     bool
}

// GetPortfolio returns the member's slice of every cabal, merged by stock.
//
// A member's share of a holding is their share units over the pot's share base times the
// cabal's position, valued at the mark the pot valuation used. P&L per holding is that
// value less the same slice of the cabal's fill-derived cost basis. Cash is what is left of
// the member's slice once the holdings are taken out, so the parts always add up to the
// total and the total is the figure Home shows.
func (p *PortfolioService) GetPortfolio(ctx context.Context, accessToken string) (PortfolioResult, error) {
	h := p.home
	ctx = HomeContextWithPotNavCache(ctx)
	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return PortfolioResult{}, err
	}
	// Same reconcile Home runs first, so both screens value the same pot.
	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return PortfolioResult{}, err
	}

	cabals, err := p.valueJoinedCabals(ctx, user.ID, joinedGroupIDs)
	if err != nil {
		return PortfolioResult{}, err
	}

	result := PortfolioResult{Holdings: []PortfolioHolding{}}
	var netIn int64
	byKey := make(map[string]*PortfolioHolding)
	order := make([]string, 0)
	for _, cabal := range cabals {
		if !cabal.valued {
			result.UnvaluedCabals++
			continue
		}
		lines, equity, err := p.cabalSlice(ctx, cabal)
		if err != nil {
			slog.Warn("portfolio: cabal slice could not be computed",
				"group_id", cabal.groupID, "user_id", user.ID, "err", err)
			result.UnvaluedCabals++
			continue
		}
		netIn += cabal.position.AmountDeposited - cabal.position.AmountWithdrawn
		result.TotalMicros += equity
		var inHoldings int64
		for _, line := range lines {
			inHoldings += line.value
			holding, seen := byKey[line.symbol]
			if !seen {
				holding = &PortfolioHolding{
					Symbol:  line.symbol,
					Name:    line.name,
					Kind:    line.kind,
					LogoURL: line.logoURL,
				}
				byKey[line.symbol] = holding
				order = append(order, line.symbol)
			}
			holding.ValueMicros += line.value
			holding.costMicros += line.cost
			holding.DollarPnLMicros += line.value - line.cost
			holding.Cabals = append(holding.Cabals, PortfolioCabalLine{
				GroupID:         cabal.groupID,
				Name:            cabal.name,
				Tint:            CabalTintName(cabal.groupID),
				PictureURL:      cabal.pictureURL,
				ValueMicros:     line.value,
				Quantity:        line.quantity,
				DollarPnLMicros: line.value - line.cost,
				costMicros:      line.cost,
			})
		}
		// Cash is the rest of the slice. Taking it as the remainder rather than a separate
		// floor keeps the parts summing to the total Home shows.
		if cash := equity - inHoldings; cash > 0 {
			result.CashMicros += cash
		}
	}

	result.DollarPnLMicros = result.TotalMicros - netIn
	if netIn > 0 {
		if pct := domain.PercentReturn(domain.USDCMicros(result.TotalMicros), domain.USDCMicros(netIn)); pct != nil {
			result.PercentReturn = formatPercentReturnDecimal(*pct)
		}
	}

	for _, key := range order {
		holding := byKey[key]
		if holding.ValueMicros <= 0 {
			continue
		}
		holding.ShareOfTotal = shareOfTotalDecimal(holding.ValueMicros, result.TotalMicros)
		if holding.costMicros > 0 {
			ratio := float64(holding.DollarPnLMicros) / float64(holding.costMicros)
			holding.PercentReturn = formatPercentReturnDecimal(ratio)
		}
		sort.SliceStable(holding.Cabals, func(a, b int) bool {
			return holding.Cabals[a].ValueMicros > holding.Cabals[b].ValueMicros
		})
		result.Holdings = append(result.Holdings, *holding)
	}
	// Biggest position first, the way a brokerage lists what you own.
	sort.SliceStable(result.Holdings, func(a, b int) bool {
		return result.Holdings[a].ValueMicros > result.Holdings[b].ValueMicros
	})

	result.AccountBalanceMicros = p.accountBalance(ctx, user.ID)
	slog.Info("portfolio read",
		"user_id", user.ID,
		"cabals", len(cabals),
		"holdings", len(result.Holdings),
		"unvalued_cabals", result.UnvaluedCabals,
	)
	return result, nil
}

// valueJoinedCabals values every joined cabal the member holds a slice of, in parallel.
// A cabal the member has no share units in is still returned, unvalued-but-harmless: it adds
// its realized result to the all-time P&L and nothing to the holdings.
func (p *PortfolioService) valueJoinedCabals(ctx context.Context, userID string, groupIDs []string) ([]portfolioCabal, error) {
	h := p.home
	if len(groupIDs) == 0 {
		return nil, nil
	}
	positions, err := h.store.ListPositionsForUserInGroups(ctx, userID, groupIDs)
	if err != nil {
		return nil, err
	}

	cabals := make([]portfolioCabal, len(groupIDs))
	errs := make([]error, len(groupIDs))
	var wg sync.WaitGroup
	for i, groupID := range groupIDs {
		wg.Add(1)
		go func(i int, groupID string) {
			defer wg.Done()
			group, found, err := h.store.GetGroupByID(ctx, groupID)
			if err != nil {
				errs[i] = err
				return
			}
			if !found {
				return
			}
			cabal := portfolioCabal{
				groupID:    groupID,
				name:       group.Name,
				pictureURL: nullStringValue(group.PictureURL),
				position:   positions[groupID],
			}
			if cabal.position.ShareUnits <= 0 {
				// Nothing of this pot is theirs now; its realized result still counts.
				cabal.valued = true
				cabals[i] = cabal
				return
			}
			valuation, err := p.valueCabal(ctx, groupID)
			if err != nil {
				slog.Warn("portfolio: cabal pot could not be valued", "group_id", groupID, "err", err)
				cabals[i] = cabal
				return
			}
			cabal.valuation = valuation
			cabal.valued = true
			cabals[i] = cabal
		}(i, groupID)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	out := make([]portfolioCabal, 0, len(cabals))
	for _, cabal := range cabals {
		if cabal.groupID != "" {
			out = append(out, cabal)
		}
	}
	return out, nil
}

// valueCabal is the pot valuation Home's figure is built from: the same treasury read, the
// same ledger, the same marks, through valuePot. It keeps the per-holding marks Home drops.
func (p *PortfolioService) valueCabal(ctx context.Context, groupID string) (potValuation, error) {
	h := p.home
	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return potValuation{}, err
	}
	treasuryUSDC, err := h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return potValuation{}, err
	}
	treasuryAddress := ""
	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return potValuation{}, err
	}
	if found {
		treasuryAddress = treasury.SolanaAddress
	}
	return valuePot(ctx, h.store, h.pyth, h.symbols, nil, groupID, treasuryAddress, treasuryUSDC, potMarksBestAvailable)
}

// sliceLine is the member's slice of one of a cabal's holdings.
type sliceLine struct {
	symbol   string
	name     string
	kind     string
	logoURL  string
	value    int64
	cost     int64
	quantity string
}

// cabalSlice splits the member's slice of one pot into its holdings, and returns the slice's
// total value (the member's equity, computed exactly as Home computes it).
func (p *PortfolioService) cabalSlice(ctx context.Context, cabal portfolioCabal) ([]sliceLine, int64, error) {
	units := cabal.position.ShareUnits
	if units <= 0 {
		return nil, 0, nil
	}
	base := cabal.valuation.ShareBaseMicros
	equity, err := shareOfPotMicros(units, cabal.valuation.PotNavMicros, base)
	if err != nil {
		return nil, 0, err
	}
	lines := make([]sliceLine, 0, len(cabal.valuation.Marked.Holdings))
	for _, holding := range cabal.valuation.Marked.Holdings {
		if holding.Units <= 0 {
			continue
		}
		decimals := pyth.NormalizeTokenDecimals(holding.Decimals)
		value, err := pyth.HoldingValueUSDCMicros(holding.Units, holding.MarkUsdc, decimals, holding.UiMultiplier, holding.Kind)
		if err != nil {
			return nil, 0, fmt.Errorf("value %s holding: %w", holding.Symbol, err)
		}
		sliceValue, err := shareOfPotMicros(units, value, base)
		if err != nil {
			return nil, 0, err
		}
		sliceCost, err := shareOfPotMicros(units, holding.CostBasis, base)
		if err != nil {
			return nil, 0, err
		}
		sliceAtomics, err := shareOfPotMicros(units, holding.Units, base)
		if err != nil {
			return nil, 0, err
		}
		quantity, err := pyth.TokenAtomicsToScaledDecimalUnits(sliceAtomics, decimals, holding.UiMultiplier, holding.Kind)
		if err != nil {
			return nil, 0, fmt.Errorf("scale %s slice: %w", holding.Symbol, err)
		}
		if sliceValue <= 0 && sliceAtomics <= 0 {
			continue
		}
		meta := p.assetMeta(ctx, holding.Mint, holding.Symbol, holding.Kind)
		lines = append(lines, sliceLine{
			symbol:   meta.symbol,
			name:     meta.name,
			kind:     meta.kind,
			logoURL:  meta.logoURL,
			value:    sliceValue,
			cost:     sliceCost,
			quantity: string(quantity),
		})
	}
	return lines, equity, nil
}

// accountBalance is the member's account balance as GET /v1/me/balance reports it, or nil
// when it cannot be read right now.
func (p *PortfolioService) accountBalance(ctx context.Context, userID string) *int64 {
	h := p.home
	if h.deposits == nil {
		return nil
	}
	wallet, found, err := h.store.GetMemberWalletByUserID(ctx, userID)
	if err != nil || !found {
		if err != nil {
			slog.Warn("portfolio: member wallet lookup failed", "user_id", userID, "err", err)
		}
		return nil
	}
	available, _, err := h.deposits.platformBalanceForWallet(ctx, userID, wallet.SolanaAddress)
	if err != nil {
		slog.Warn("portfolio: account balance unavailable", "user_id", userID, "err", err)
		return nil
	}
	return &available
}

// assetMetaRow is how a mint reads to a member: ticker, name, kind and logo.
type assetMetaRow struct {
	symbol  string
	name    string
	kind    string
	logoURL string
}

// assetMeta looks a mint up in the catalogue. The ticker falls back to what the valuation
// already resolved and the name to the ticker, so a catalogue outage costs the row its name,
// never the row.
func (p *PortfolioService) assetMeta(ctx context.Context, mint, fallbackSymbol string, fallbackKind xstocks.AssetKind) assetMetaRow {
	h := p.home
	meta := assetMetaRow{symbol: strings.TrimSpace(fallbackSymbol), kind: string(fallbackKind)}
	if meta.symbol == "" {
		meta.symbol = symbolForOutputMint(ctx, h.symbols, mint)
	}
	if meta.kind == "" {
		meta.kind = string(xstocks.AssetKindStock)
	}
	if h.symbols != nil && h.symbols.catalog != nil && strings.TrimSpace(mint) != "" {
		if asset, found, err := h.symbols.catalog.LookupByMint(ctx, mint); err == nil && found {
			asset = asset.Normalize()
			if symbol := strings.TrimSpace(asset.Symbol); symbol != "" {
				meta.symbol = symbol
			}
			meta.name = strings.TrimSpace(asset.Name)
			if asset.Kind != "" {
				meta.kind = string(asset.Kind)
			}
			meta.logoURL = strings.TrimSpace(asset.LogoURL)
		}
	}
	if meta.name == "" {
		meta.name = meta.symbol
	}
	return meta
}

// shareOfTotalDecimal is part / total as a decimal string with six places, trimmed.
func shareOfTotalDecimal(part, total int64) string {
	if part <= 0 || total <= 0 {
		return "0"
	}
	if part >= total {
		return "1"
	}
	millionths, err := domain.MulDivFloor(part, 1_000_000, total)
	if err != nil {
		return "0"
	}
	formatted := strings.TrimRight(fmt.Sprintf("0.%06d", millionths), "0")
	formatted = strings.TrimSuffix(formatted, ".")
	if formatted == "0" || formatted == "" {
		return "0"
	}
	return formatted
}

// FormatUsdDecimal is micros as the API's dollar string ("1248.50"), the same rounding every
// other money figure in the API uses, so a portfolio total reads exactly as Home's does.
func FormatUsdDecimal(micros int64) string {
	return formatMicrosAsUsdDecimal(micros)
}

// FormatSignedUsdDecimal is micros as the API's signed dollar P&L string ("+48.20", "-7.60").
func FormatSignedUsdDecimal(micros int64) string {
	return formatSignedDollarPnL(micros)
}
