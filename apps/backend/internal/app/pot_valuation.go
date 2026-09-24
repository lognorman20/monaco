package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/packages/domain"
)

// ErrPotMarkUnavailable means a holding could not be priced from a live source. Anything
// that mints shares or pays USDC must stop on it and retry later: cost basis is what the
// pot paid, not what it is worth.
var ErrPotMarkUnavailable = errors.New("live pot mark unavailable")

// potMarkPolicy says which marks a valuation may be built from.
type potMarkPolicy int

const (
	// potMarksBestAvailable accepts the price chain's cost-basis fallback. Screens and NAV
	// history use it: they must keep rendering while an oracle is down.
	potMarksBestAvailable potMarkPolicy = iota
	// potMarksLiveOnly fails with ErrPotMarkUnavailable instead of falling back. Deposit
	// credit, redeem pricing and surplus credit use it.
	potMarksLiveOnly
)

// potValuation is the one valuation of a group pot. Deposit credit, redeem quote and pay,
// NAV snapshots and the group screens all read it, so they cannot price the pot differently.
type potValuation struct {
	// TreasuryUSDC is the on-chain balance the caller read.
	TreasuryUSDC int64
	// CashMicros is the part of TreasuryUSDC that belongs to the share holders: on-chain
	// USDC capped at what the group's ledger accounts for. USDC above the ledger has
	// landed without being credited yet (a sweep awaiting confirmation, a stray
	// transfer) and is nobody's gain.
	CashMicros int64
	// HoldingsMicros is the marked value of every xStock holding.
	HoldingsMicros int64
	// PotNavMicros is CashMicros + HoldingsMicros.
	PotNavMicros int64
	// ShareBaseMicros is every claim on the pot: position share units plus units already
	// debited by redeem jobs that have not paid out yet.
	ShareBaseMicros int64
	// Marked carries the per-holding marks behind HoldingsMicros.
	Marked pyth.NavInput
}

// navSnapshotValues is the snapshot row for this valuation.
func (v potValuation) navSnapshotValues() (postgres.NavSnapshotValues, error) {
	return navSnapshotValuesFor(v.PotNavMicros, v.ShareBaseMicros)
}

func navSnapshotValuesFor(potNavMicros, shareBaseMicros int64) (postgres.NavSnapshotValues, error) {
	if potNavMicros < 0 || shareBaseMicros < 0 {
		return postgres.NavSnapshotValues{}, fmt.Errorf("pot nav and share base must be non-negative")
	}
	perShare := int64(domain.BootstrapSharePriceMicros)
	if shareBaseMicros > 0 {
		var err error
		perShare, err = domain.MulDivFloor(potNavMicros, 1_000_000, shareBaseMicros)
		if err != nil {
			return postgres.NavSnapshotValues{}, fmt.Errorf("nav per share: %w", err)
		}
	}
	return postgres.NavSnapshotValues{
		PotNavMicros:      potNavMicros,
		NavPerShareMicros: perShare,
		TotalShares:       shareBaseMicros,
	}, nil
}

// valuePot values a group pot from the on-chain treasury balance, the group's ledger and the
// marks the policy allows. tx, when set, makes the ledger reads see the caller's open work.
func valuePot(
	ctx context.Context,
	store *postgres.Store,
	pythClient pyth.Client,
	symbols *SymbolResolver,
	tx *sql.Tx,
	groupID, treasuryAddress string,
	treasuryUSDC int64,
	policy potMarkPolicy,
) (potValuation, error) {
	if groupID == "" {
		return potValuation{}, fmt.Errorf("group_id is required")
	}
	if treasuryUSDC < 0 {
		return potValuation{}, fmt.Errorf("treasury usdc must be non-negative")
	}

	var (
		positionShares, jobShares, ledgerUSDC int64
		holdings                              []postgres.TokenHoldingRow
		err                                   error
	)
	if tx != nil {
		if positionShares, err = store.SumShareUnitsByGroupTx(ctx, tx, groupID); err != nil {
			return potValuation{}, err
		}
		if jobShares, err = store.SumActiveRedeemJobShareUnitsTx(ctx, tx, groupID); err != nil {
			return potValuation{}, err
		}
		if ledgerUSDC, err = store.PotLedgerUSDCTx(ctx, tx, groupID); err != nil {
			return potValuation{}, err
		}
		if holdings, err = store.ListNetTokenHoldingsByGroupTx(ctx, tx, groupID); err != nil {
			return potValuation{}, err
		}
	} else {
		if positionShares, err = store.SumShareUnitsByGroup(ctx, groupID); err != nil {
			return potValuation{}, err
		}
		if jobShares, err = store.SumActiveRedeemJobShareUnits(ctx, groupID); err != nil {
			return potValuation{}, err
		}
		if ledgerUSDC, err = store.PotLedgerUSDC(ctx, groupID); err != nil {
			return potValuation{}, err
		}
		if holdings, err = store.ListNetTokenHoldingsByGroup(ctx, groupID); err != nil {
			return potValuation{}, err
		}
	}

	cash := treasuryUSDC
	if ledgerUSDC < 0 {
		ledgerUSDC = 0
	}
	if cash > ledgerUSDC {
		cash = ledgerUSDC
	}

	valuation := potValuation{
		TreasuryUSDC:    treasuryUSDC,
		CashMicros:      cash,
		PotNavMicros:    cash,
		ShareBaseMicros: positionShares + jobShares,
		Marked:          pyth.NavInput{TreasuryUsdc: treasuryUSDC},
	}
	if len(holdings) == 0 {
		return valuation, nil
	}

	marked, err := fetchMarkedPotInput(ctx, store, pythClient, symbols, tx, groupID, treasuryAddress, treasuryUSDC, holdings, policy)
	if err != nil {
		return potValuation{}, err
	}
	shareBase, err := domain.ShareUnitsMicrosToDomain(valuation.ShareBaseMicros)
	if err != nil {
		return potValuation{}, err
	}
	navInput, err := domainNavInputFromPyth(marked, shareBase)
	if err != nil {
		return potValuation{}, err
	}
	navInput.TreasuryUsdc = domain.USDCMicros(cash)
	nav, err := ComputePotNAV(navInput)
	if err != nil {
		return potValuation{}, fmt.Errorf("compute pot nav: %w", err)
	}

	valuation.PotNavMicros = int64(nav.TotalUsdc)
	valuation.HoldingsMicros = valuation.PotNavMicros - cash
	valuation.Marked = marked
	return valuation, nil
}

// marksForLedgerHoldings pairs every holding on the group's ledger with the mark the price
// client returned for it. Units and cost basis always come from the ledger: the price client
// only contributes a price. Under potMarksLiveOnly a holding with no live mark is
// ErrPotMarkUnavailable; otherwise it is carried at cost basis.
func marksForLedgerHoldings(groupID string, ledger []pyth.CostBasis, marked []pyth.MarkedHolding, policy potMarkPolicy) ([]pyth.MarkedHolding, error) {
	out := make([]pyth.MarkedHolding, 0, len(ledger))
	for _, holding := range ledger {
		mark, found := markForHolding(holding, marked)
		_, multResolved := pyth.EffectiveUiMultiplier(holding.UiMultiplier, holding.Kind)
		live := found && mark.MarkUsdc > 0 && mark.Source != pyth.MarkSourceCostBasis && multResolved
		if !live && policy == potMarksLiveOnly {
			if !multResolved {
				return nil, fmt.Errorf("%w: %s multiplier unresolved (group %s)", ErrPotMarkUnavailable, holding.Symbol, groupID)
			}
			return nil, fmt.Errorf("%w: %s has no live price (group %s)", ErrPotMarkUnavailable, holding.Symbol, groupID)
		}
		if !found || mark.MarkUsdc <= 0 {
			costMark, err := pyth.CostBasisMarkPerUnitMicros(holding.Price, holding.Amount, holding.Decimals, holding.UiMultiplier, holding.Kind)
			if err != nil {
				return nil, err
			}
			mark = pyth.MarkedHolding{MarkUsdc: costMark, Source: pyth.MarkSourceCostBasis}
		}
		mark.Symbol = holding.Symbol
		mark.Mint = holding.Mint
		mark.Units = holding.Units
		mark.CostBasis = holding.Price
		mark.Decimals = holding.Decimals
		mark.Kind = holding.Kind
		mark.UiMultiplier = holding.UiMultiplier
		out = append(out, mark)
	}
	return out, nil
}

func markForHolding(holding pyth.CostBasis, marked []pyth.MarkedHolding) (pyth.MarkedHolding, bool) {
	for _, candidate := range marked {
		if candidate.Mint != "" && candidate.Mint == holding.Mint {
			return candidate, true
		}
	}
	for _, candidate := range marked {
		if candidate.Mint == "" && candidate.Symbol == holding.Symbol {
			return candidate, true
		}
	}
	return pyth.MarkedHolding{}, false
}

func logPotMarkUnavailable(groupID, stage string, err error) {
	slog.Warn("pot valuation deferred: live mark unavailable",
		"group_id", groupID,
		"stage", stage,
		"err", err,
	)
}
