package domain

import "fmt"

// NavMode selects how pot NAV is computed.
type NavMode int

const (
	// NavUSDCOnly values the pot from treasury USDC only (M2).
	NavUSDCOnly NavMode = iota
	// NavMarked values treasury USDC plus marked xStock holdings (M4).
	NavMarked
)

// BootstrapSharePriceMicros is the $1/share price used before any shares exist.
const BootstrapSharePriceMicros = 1_000_000

// NavInput is the pure domain input for pot valuation.
type NavInput struct {
	Mode         NavMode
	TreasuryUsdc USDCMicros
	TotalShares  ShareUnits
	Holdings     []MarkedHolding // ignored unless NavMarked
}

// MarkedHolding is one xStock line in a marked pot.
// CostBasis comes from transactions; MarkUsdc is the live Pyth mark per unit.
type MarkedHolding struct {
	Symbol     string
	Units      string
	MarkUsdc   USDCMicros
	CostBasis  USDCMicros
	AfterHours bool
}

// PotNAV is the group pot value and per-share price.
type PotNAV struct {
	TotalUsdc    USDCMicros
	PerShareUsdc USDCMicros
}

// ComputePotNAV returns treasury USDC plus marked xStock value when mode is NavMarked.
// Per-share price is pot NAV divided by total shares, or $1 when no shares exist yet.
func ComputePotNAV(in NavInput) (PotNAV, error) {
	if in.TreasuryUsdc < 0 {
		return PotNAV{}, fmt.Errorf("treasury usdc must be non-negative")
	}

	total := in.TreasuryUsdc
	if in.Mode == NavMarked {
		for _, holding := range in.Holdings {
			value, err := multiplyDecimalByMicros(holding.Units, holding.MarkUsdc)
			if err != nil {
				return PotNAV{}, err
			}
			if holding.MarkUsdc < 0 {
				return PotNAV{}, fmt.Errorf("mark for %s must be non-negative", holding.Symbol)
			}
			// Checked add: a wrapped total would turn a large pot negative and
			// then price every share off a negative NAV.
			total, err = AddMicros(total, value)
			if err != nil {
				return PotNAV{}, fmt.Errorf("pot nav for %s: %w", holding.Symbol, err)
			}
		}
	}

	perShare, err := perShareUsdc(in.TotalShares, total)
	if err != nil {
		return PotNAV{}, err
	}

	return PotNAV{
		TotalUsdc:    total,
		PerShareUsdc: perShare,
	}, nil
}

func perShareUsdc(totalShares ShareUnits, potNav USDCMicros) (USDCMicros, error) {
	if totalShares.IsZero() {
		return USDCMicros(BootstrapSharePriceMicros), nil
	}
	return divideMicrosByShares(potNav, totalShares)
}
