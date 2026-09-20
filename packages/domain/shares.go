package domain

import (
	"fmt"
	"math/big"
	"strings"
)

// SharesForDeposit mints claim units from swept USDC against the pot as it stood before the credit.
func SharesForDeposit(deposited USDCMicros, totalSharesMicros int64, preCreditNav USDCMicros) (ShareUnits, error) {
	micros, err := ShareUnitsMicrosForDeposit(deposited, totalSharesMicros, preCreditNav)
	if err != nil {
		return "", err
	}
	return shareUnitsFromMicros(micros)
}

// ShareUnitsMicrosForDeposit returns the positions.share_units increment for a deposit:
// deposited × totalShares / preCreditNav, floored so rounding never favours the depositor
// over the members already in the pot. Shares are minted 1:1 only while none are outstanding;
// a pot that already has shares is always priced at its NAV, cash-only or not.
func ShareUnitsMicrosForDeposit(deposited USDCMicros, totalSharesMicros int64, preCreditNav USDCMicros) (int64, error) {
	if deposited <= 0 {
		return 0, fmt.Errorf("deposited must be positive")
	}
	if totalSharesMicros < 0 {
		return 0, fmt.Errorf("total shares must be non-negative")
	}
	if totalSharesMicros == 0 {
		return int64(deposited), nil
	}
	if preCreditNav <= 0 {
		return 0, fmt.Errorf("pre-credit pot nav must be positive while shares are outstanding")
	}
	minted, err := MulDivFloor(int64(deposited), totalSharesMicros, int64(preCreditNav))
	if err != nil {
		return 0, fmt.Errorf("mint share units: %w", err)
	}
	if minted <= 0 {
		return 0, fmt.Errorf("deposit below minimum share increment")
	}
	return minted, nil
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
