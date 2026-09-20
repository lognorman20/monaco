package domain

import (
	"fmt"
	"math/big"
	"strings"
)

// SharesForDeposit mints claim units from swept USDC at the current pot NAV per share.
// Invariant: shares = deposited / nav.PerShareUsdc (ledger stores micro-scaled units).
func SharesForDeposit(deposited USDCMicros, nav PotNAV) (ShareUnits, error) {
	micros, err := ShareUnitsMicrosForDeposit(deposited, nav)
	if err != nil {
		return "", err
	}
	return shareUnitsFromMicros(micros)
}

// ShareUnitsMicrosForDeposit returns the positions.share_units increment for a deposit.
func ShareUnitsMicrosForDeposit(deposited USDCMicros, nav PotNAV) (int64, error) {
	if deposited <= 0 {
		return 0, fmt.Errorf("deposited must be positive")
	}
	if nav.PerShareUsdc <= 0 {
		return 0, fmt.Errorf("per-share usdc must be positive")
	}
	product := new(big.Rat).Mul(
		big.NewRat(int64(deposited), 1),
		big.NewRat(1_000_000, 1),
	)
	quotient := new(big.Rat).Quo(product, big.NewRat(int64(nav.PerShareUsdc), 1))
	micros, err := ratRoundToInt64(quotient)
	if err != nil {
		return 0, fmt.Errorf("share units for deposit: %w", err)
	}
	return micros, nil
}

func shareUnitsFromMicros(micros int64) (ShareUnits, error) {
	if micros < 0 {
		return "", fmt.Errorf("share units must be non-negative")
	}
	if micros == 0 {
		return ShareUnits("0"), nil
	}
	r := new(big.Rat).SetFrac(big.NewInt(micros), big.NewInt(1_000_000))
	return ShareUnits(trimTrailingZeros(r.FloatString(6))), nil
}

func ShareUnitsMicrosToDomain(micros int64) (ShareUnits, error) {
	return shareUnitsFromMicros(micros)
}

func trimTrailingZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	return strings.TrimRight(value, ".")
}
