package app

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/packages/domain"
)

// HeldAssetCabal is one cabal's position in one symbol.
type HeldAssetCabal struct {
	GroupID   string
	Name      string
	Units     string
	ValueUsd  string
	DollarPnL string
	// MySliceUsd is the caller's own share of this position, by share units — the
	// same arithmetic the home dashboard does for a whole pot, applied to one line
	// of it.
	MySliceUsd string
}

// HeldAsset is one symbol at least one of the caller's cabals holds.
type HeldAsset struct {
	Symbol         string
	Cabals         []HeldAssetCabal
	TotalValueUsd  string
	TotalDollarPnL string
	MySliceUsd     string

	totalValueMicros int64
	totalPnLMicros   int64
	mySliceMicros    int64
}

// VotableAsset is one symbol with an open proposal in one of the caller's cabals.
type VotableAsset struct {
	Symbol        string
	OpenProposals int
	CabalNames    []string
	// SoonestExpiresAt is when the first of those votes closes, in UTC.
	SoonestExpiresAt *time.Time
}

// HeldAssetsResult is GET /v1/assets/held.
type HeldAssetsResult struct {
	Held      []HeldAsset
	UpForVote []VotableAsset
}

// heldCabalRow is one cabal's line in one symbol, before the symbols are folded.
type heldCabalRow struct {
	symbol      string
	groupID     string
	groupName   string
	units       string
	valueMicros int64
	pnlMicros   int64
	sliceMicros int64
}

// openProposalRow is one open vote, with the cabal it belongs to named.
type openProposalRow struct {
	symbol    string
	groupName string
	expiresAt time.Time
}

// GetHeldAssets answers "what do my cabals own, and what are they voting on" in
// one pass over the caller's cabals.
//
// One route rather than a group-view call per cabal from the client: the Stocks
// tab needs both sections before its first scroll, and N round trips from a phone
// is N chances to arrive after the member has moved on.
//
// A cabal that cannot be valued is left out rather than failing the whole answer,
// and its open votes are still read — the two are separate failures. The
// alternative is an empty "In your cabals" because one unrelated cabal's treasury
// did not respond, which reads as "you own nothing", a lie about someone's money.
// Marks are best-available for the same reason: this is a display aggregate and
// nothing here moves funds.
func (h *HomeService) GetHeldAssets(ctx context.Context, accessToken string) (HeldAssetsResult, error) {
	ctx = HomeContextWithPotNavCache(ctx)
	user, joinedGroupIDs, err := h.authenticateHomeUser(ctx, accessToken)
	if err != nil {
		return HeldAssetsResult{}, err
	}
	empty := HeldAssetsResult{Held: []HeldAsset{}, UpForVote: []VotableAsset{}}
	if len(joinedGroupIDs) == 0 {
		return empty, nil
	}

	if err := h.creditUncreditedForGroups(ctx, joinedGroupIDs); err != nil {
		return HeldAssetsResult{}, err
	}

	type groupScan struct {
		holdings  []heldCabalRow
		proposals []openProposalRow
	}
	scans := make([]groupScan, len(joinedGroupIDs))

	var wg sync.WaitGroup
	for i, groupID := range joinedGroupIDs {
		wg.Add(1)
		go func(i int, groupID string) {
			defer wg.Done()
			group, found, err := h.store.GetGroupByID(ctx, groupID)
			if err != nil || !found {
				return
			}
			if holdings, err := h.heldRowsForGroup(ctx, user.ID, groupID, group.Name); err == nil {
				scans[i].holdings = holdings
			} else {
				logHeldScanFailed(groupID, err)
			}
			if proposals, err := h.openProposalsForGroup(ctx, groupID, group.Name); err == nil {
				scans[i].proposals = proposals
			} else {
				logHeldScanFailed(groupID, err)
			}
		}(i, groupID)
	}
	wg.Wait()

	holdings := []heldCabalRow{}
	proposals := []openProposalRow{}
	for _, scan := range scans {
		holdings = append(holdings, scan.holdings...)
		proposals = append(proposals, scan.proposals...)
	}

	return HeldAssetsResult{
		Held:      foldHeldRows(holdings),
		UpForVote: foldOpenProposals(proposals),
	}, nil
}

// heldRowsForGroup values one cabal's pot and works out the caller's slice of
// each position in it.
func (h *HomeService) heldRowsForGroup(ctx context.Context, userID, groupID, groupName string) ([]heldCabalRow, error) {
	treasury, found, err := h.store.GetTreasuryByGroupID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return nil, err
	}
	treasuryUSDC, err := h.groupTreasuryUSDC(ctx, groupID, netUsdcIn)
	if err != nil {
		return nil, err
	}

	potView, err := computeGroupPotView(ctx, h.store, h.pyth, h.symbols, groupID, treasury.SolanaAddress, treasuryUSDC)
	if err != nil {
		return nil, err
	}

	positionRow, hasPosition, err := h.store.GetPosition(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	shareUnitsMicro := int64(0)
	if hasPosition {
		shareUnitsMicro = positionRow.ShareUnits
	}

	rows := make([]heldCabalRow, 0, len(potView.Rows))
	for _, row := range potView.Rows {
		symbol := strings.TrimSpace(row.Symbol)
		// Cash is not a holding anyone opens a stock screen for.
		if symbol == "" || strings.EqualFold(symbol, "USDC") || row.ValueUsdcMicros <= 0 {
			continue
		}
		// The caller's slice of this one position, by the same share-unit ratio
		// that divides the whole pot. A missing share base means the cabal has no
		// units issued yet, in which case the slice is nothing, not everything.
		slice, err := shareOfPotMicros(shareUnitsMicro, row.ValueUsdcMicros, potView.ShareBaseMicros)
		if err != nil {
			slice = 0
		}
		rows = append(rows, heldCabalRow{
			symbol:      symbol,
			groupID:     groupID,
			groupName:   groupName,
			units:       row.Units,
			valueMicros: row.ValueUsdcMicros,
			pnlMicros:   row.DollarPnLUsdcMicros,
			sliceMicros: slice,
		})
	}
	return rows, nil
}

func (h *HomeService) openProposalsForGroup(ctx context.Context, groupID, groupName string) ([]openProposalRow, error) {
	proposals, err := h.store.ListProposalsByGroupID(ctx, groupID, []domain.ProposalStatus{domain.ProposalOpen})
	if err != nil {
		return nil, err
	}
	rows := make([]openProposalRow, 0, len(proposals))
	for _, proposal := range proposals {
		symbol := strings.TrimSpace(proposal.Symbol)
		if symbol == "" {
			continue
		}
		rows = append(rows, openProposalRow{
			symbol:    symbol,
			groupName: groupName,
			expiresAt: proposal.ExpiresAt.UTC(),
		})
	}
	return rows, nil
}

// foldHeldRows turns per-cabal lines into one row per symbol, biggest position
// first — the member's own money leads the section.
func foldHeldRows(rows []heldCabalRow) []HeldAsset {
	byUpperSymbol := map[string]*HeldAsset{}
	order := []string{}
	for _, row := range rows {
		key := strings.ToUpper(row.symbol)
		entry, found := byUpperSymbol[key]
		if !found {
			entry = &HeldAsset{Symbol: row.symbol}
			byUpperSymbol[key] = entry
			order = append(order, key)
		}
		entry.Cabals = append(entry.Cabals, HeldAssetCabal{
			GroupID:    row.groupID,
			Name:       row.groupName,
			Units:      row.units,
			ValueUsd:   formatMicrosAsUsdDecimal(row.valueMicros),
			DollarPnL:  formatSignedDollarPnL(row.pnlMicros),
			MySliceUsd: formatMicrosAsUsdDecimal(row.sliceMicros),
		})
		entry.totalValueMicros += row.valueMicros
		entry.totalPnLMicros += row.pnlMicros
		entry.mySliceMicros += row.sliceMicros
	}

	out := make([]HeldAsset, 0, len(order))
	for _, key := range order {
		entry := byUpperSymbol[key]
		entry.TotalValueUsd = formatMicrosAsUsdDecimal(entry.totalValueMicros)
		entry.TotalDollarPnL = formatSignedDollarPnL(entry.totalPnLMicros)
		entry.MySliceUsd = formatMicrosAsUsdDecimal(entry.mySliceMicros)
		out = append(out, *entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].totalValueMicros > out[j].totalValueMicros
	})
	return out
}

// foldOpenProposals turns open votes into one row per symbol, closing soonest
// first — a vote is a deadline, so the urgent one leads.
func foldOpenProposals(rows []openProposalRow) []VotableAsset {
	byUpperSymbol := map[string]*VotableAsset{}
	order := []string{}
	for _, row := range rows {
		key := strings.ToUpper(row.symbol)
		entry, found := byUpperSymbol[key]
		if !found {
			entry = &VotableAsset{Symbol: row.symbol}
			byUpperSymbol[key] = entry
			order = append(order, key)
		}
		entry.OpenProposals++
		if row.groupName != "" && !containsString(entry.CabalNames, row.groupName) {
			entry.CabalNames = append(entry.CabalNames, row.groupName)
		}
		expires := row.expiresAt
		if entry.SoonestExpiresAt == nil || expires.Before(*entry.SoonestExpiresAt) {
			entry.SoonestExpiresAt = &expires
		}
	}

	out := make([]VotableAsset, 0, len(order))
	for _, key := range order {
		out = append(out, *byUpperSymbol[key])
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i].SoonestExpiresAt, out[j].SoonestExpiresAt
		if left == nil || right == nil {
			return left != nil && right == nil
		}
		return left.Before(*right)
	})
	return out
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
