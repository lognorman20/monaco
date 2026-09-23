package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/monaco/monaco/apps/backend/internal/marks"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/packages/domain"
)

// assetHoldings values the viewer's cabals' position in one symbol.
//
// Every read here is set-based: the number of queries is the same whether the viewer
// is in one cabal or twenty, and none of them touches the chain. The card asks a
// narrow question — how many units of this one stock does each of my cabals hold,
// what did they pay, what is it worth, and how much of that is mine — and the answer
// needs the ledger and a mark, not a pot valuation. Valuing the whole pot per cabal
// would make this an N+1 and would read each treasury's on-chain USDC balance to
// compute a NAV the card then throws away.
//
// A cabal that cannot be valued is counted, never dropped: showing a member a short
// list with no caveat tells them their other cabals hold nothing, which is a lie
// about their money.
func (h *HomeService) assetHoldings(ctx context.Context, userID string, groupIDs []string, symbol string) ([]AssetSocialHolding, int) {
	holdings, unvalued, err := h.assetHoldingsForGroups(ctx, userID, groupIDs, symbol)
	if err != nil {
		// The reads are set-based, so a failure is the whole set failing, not one
		// cabal. The card keeps its votes and its activity and says it could not
		// check any of the pots.
		slog.Warn("asset social: cabal holdings could not be read",
			"symbol", symbol, "group_count", len(groupIDs), "err", err)
		return []AssetSocialHolding{}, len(groupIDs)
	}
	return holdings, unvalued
}

func (h *HomeService) assetHoldingsForGroups(
	ctx context.Context,
	userID string,
	groupIDs []string,
	symbol string,
) ([]AssetSocialHolding, int, error) {
	empty := []AssetSocialHolding{}
	if len(groupIDs) == 0 {
		return empty, 0, nil
	}

	ledger, err := h.store.ListGroupHoldings(ctx, groupIDs)
	if err != nil {
		return nil, 0, err
	}
	matches, tickerByToken := h.holdingsOfSymbol(ctx, ledger, symbol)
	if len(matches) == 0 {
		return empty, 0, nil
	}

	// Only the cabals that actually hold the stock are read further. A member in
	// twenty clubs, two of which own this one, pays for two.
	holdingGroupIDs := groupIDsOf(matches)

	names, err := h.store.ListGroupNamesByIDs(ctx, holdingGroupIDs)
	if err != nil {
		return nil, 0, err
	}
	treasuries, err := h.store.ListTreasuryAddressesByGroupIDs(ctx, holdingGroupIDs)
	if err != nil {
		return nil, 0, err
	}
	shareBase, err := h.store.ListShareBaseForGroups(ctx, holdingGroupIDs)
	if err != nil {
		return nil, 0, err
	}
	positions, err := h.store.ListPositionsForUserInGroups(ctx, userID, holdingGroupIDs)
	if err != nil {
		return nil, 0, err
	}

	marked := h.marksForHoldings(ctx, matches, tickerByToken)

	holdings := make([]AssetSocialHolding, 0, len(matches))
	unvalued := 0
	for _, match := range matches {
		name, groupFound := names[match.GroupID]
		if !groupFound {
			continue
		}
		if _, hasTreasury := treasuries[match.GroupID]; !hasTreasury {
			// No treasury row means no pot at all. That cabal does not hold the
			// stock rather than failing to be valued.
			continue
		}

		mark, ok := marked[match.Token]
		markUsdc := mark.MarkUsdc
		afterHours := mark.AfterHours
		if !ok || markUsdc <= 0 {
			// No live mark. The cabal carries the holding at what it paid, which is
			// what every other screen does when an oracle is down.
			costMark, err := costBasisMarkPerUnitMicros(match.CostBasisUsdc, match.Units)
			if err != nil {
				// Neither a price nor a cost basis: there is no honest number to
				// show, so the card says this cabal went unchecked.
				slog.Warn("asset social: cabal holding has no price and no cost basis",
					"group_id", match.GroupID, "symbol", symbol, "token", match.Token, "err", err)
				unvalued++
				continue
			}
			markUsdc = costMark
			afterHours = false
		}

		build, err := assetHoldingRow(name, match.GroupID, marks.MarkedHolding{
			Symbol:     symbol,
			Token:      match.Token,
			Units:      match.Units,
			MarkUsdc:   markUsdc,
			CostBasis:  match.CostBasisUsdc,
			AfterHours: afterHours,
		})
		if err != nil {
			slog.Warn("asset social: cabal holding could not be valued",
				"group_id", match.GroupID, "symbol", symbol, "err", err)
			unvalued++
			continue
		}

		position, hasPosition := positions[match.GroupID]
		slicePercent, sliceMicros, err := viewerSliceOfHolding(position, hasPosition, shareBase[match.GroupID], build.valueMicros)
		if err != nil {
			slog.Warn("asset social: viewer slice could not be computed",
				"group_id", match.GroupID, "symbol", symbol, "err", err)
			unvalued++
			continue
		}
		build.row.MySliceUsd = formatMicrosAsUsdDecimal(sliceMicros)
		build.row.MySlicePercent = slicePercent
		holdings = append(holdings, build.row)
	}

	// Biggest position first: the cabal with the most at stake is the one the member
	// came to look at.
	sort.SliceStable(holdings, func(a, b int) bool {
		return usdDecimalLess(holdings[b].ValueUsd, holdings[a].ValueUsd)
	})
	return holdings, unvalued, nil
}

// holdingsOfSymbol keeps the ledger rows whose token is this ticker, and returns the
// catalog's own spelling of it for each matching token.
//
// A token address is resolved once for the whole request, not once per cabal that
// holds it: the B20 catalog lookup behind symbolForOutputToken is the same answer
// every time, and it can go off-process. The catalog's spelling travels on rather
// than the caller's, because the mark is looked up by ticker and the caller's path
// segment is whatever they typed.
func (h *HomeService) holdingsOfSymbol(
	ctx context.Context,
	ledger []postgres.GroupSymbolHolding,
	symbol string,
) ([]postgres.GroupSymbolHolding, map[string]string) {
	symbol = strings.TrimSpace(symbol)
	resolvedByToken := make(map[string]string, len(ledger))
	tickerByToken := make(map[string]string)
	matches := make([]postgres.GroupSymbolHolding, 0, len(ledger))
	for _, row := range ledger {
		if row.Units <= 0 {
			continue
		}
		resolved, seen := resolvedByToken[row.Token]
		if !seen {
			resolved = strings.TrimSpace(symbolForOutputToken(ctx, h.symbols, row.Token))
			resolvedByToken[row.Token] = resolved
		}
		if strings.EqualFold(resolved, symbol) {
			matches = append(matches, row)
			tickerByToken[row.Token] = resolved
		}
	}
	return matches, tickerByToken
}

// marksForHoldings resolves one Chainlink mark per token for the whole request,
// keyed by token address.
//
// Every cabal holding the same stock is asking the price source the same question, so
// it is asked once. A token with no usable mark is absent from the map and each cabal
// then carries its holding at its own cost basis.
//
// This card is about one symbol, so in practice there is one feed behind the whole
// map: marks.Client fails the batch if any symbol fails, and when it does every
// cabal falls back together. That is the same thing the group screens do with a
// mark they could not read, and it is why the fallback is per cabal rather than a
// single shared number -- each cabal paid its own price.
//
// The treasury reference is deliberately empty. marks.Client reads a treasury's
// on-chain USDC balance when it is given an address, and this card never shows a
// pot's cash — only the per-token mark is wanted, so no chain call is made for it.
func (h *HomeService) marksForHoldings(
	ctx context.Context,
	matches []postgres.GroupSymbolHolding,
	tickerByToken map[string]string,
) map[string]marks.MarkedHolding {
	out := make(map[string]marks.MarkedHolding)
	if h.pyth == nil || len(matches) == 0 {
		return out
	}

	// One cost-basis entry per token, aggregated across cabals: the mark is per
	// token, so asking once per cabal would ask the same feed the same question
	// several times over.
	type aggregate struct {
		units int64
		usdc  int64
	}
	order := make([]string, 0, len(matches))
	byToken := make(map[string]*aggregate, len(matches))
	for _, match := range matches {
		agg, seen := byToken[match.Token]
		if !seen {
			agg = &aggregate{}
			byToken[match.Token] = agg
			order = append(order, match.Token)
		}
		agg.units += match.Units
		agg.usdc += match.CostBasisUsdc
	}

	costBasis := make([]marks.CostBasis, 0, len(order))
	for _, token := range order {
		agg := byToken[token]
		costBasis = append(costBasis, marks.CostBasis{
			Symbol: tickerByToken[token],
			Token:  token,
			Units:  agg.units,
			Price:  agg.usdc,
			Amount: agg.units,
		})
	}

	input, err := h.pyth.MarkedPot(ctx, marks.TreasuryRef{}, costBasis)
	if err != nil {
		// Same fallback the group screens use: a price outage shows holdings at cost,
		// it does not empty the card.
		slog.Warn("asset social: marked pot pricing failed; holdings fall back to cost basis", "err", err)
		return out
	}
	for _, holding := range input.Holdings {
		if holding.MarkUsdc <= 0 {
			continue
		}
		out[holding.Token] = holding
	}
	return out
}

// viewerSliceOfHolding converts the viewer's share units into their dollars of this
// one holding. A member owning a fifth of the pot owns a fifth of every position in it.
func viewerSliceOfHolding(
	position postgres.PositionRow,
	hasPosition bool,
	shareBaseMicros, holdingValueMicros int64,
) (string, int64, error) {
	if !hasPosition || position.ShareUnits <= 0 || shareBaseMicros <= 0 {
		return "0", 0, nil
	}
	sliceMicros, err := shareOfPotMicros(position.ShareUnits, holdingValueMicros, shareBaseMicros)
	if err != nil {
		return "0", 0, err
	}
	memberShares, err := domain.ShareUnitsMicrosToDomain(position.ShareUnits)
	if err != nil {
		return "0", 0, err
	}
	totalShares, err := domain.ShareUnitsMicrosToDomain(shareBaseMicros)
	if err != nil {
		return "0", 0, err
	}
	return formatShareFractionDecimal(memberShares, totalShares), sliceMicros, nil
}

// groupIDsOf returns the distinct group ids in holdings, in the order they appear.
func groupIDsOf(holdings []postgres.GroupSymbolHolding) []string {
	seen := make(map[string]struct{}, len(holdings))
	out := make([]string, 0, len(holdings))
	for _, holding := range holdings {
		if _, dup := seen[holding.GroupID]; dup {
			continue
		}
		seen[holding.GroupID] = struct{}{}
		out = append(out, holding.GroupID)
	}
	return out
}
