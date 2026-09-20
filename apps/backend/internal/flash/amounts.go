package flash

import (
	"fmt"
	"strconv"
	"strings"
)

// AtomicsToDecimal renders atomic units as the decimal string Flash expects for qty.
func AtomicsToDecimal(atomics int64, decimals int) (string, error) {
	if atomics <= 0 {
		return "", fmt.Errorf("flash: amount must be positive")
	}
	if decimals < 0 || decimals > 18 {
		return "", fmt.Errorf("flash: unsupported decimals %d", decimals)
	}
	raw := strconv.FormatInt(atomics, 10)
	if decimals == 0 {
		return raw, nil
	}
	if len(raw) <= decimals {
		raw = strings.Repeat("0", decimals-len(raw)+1) + raw
	}
	whole, frac := raw[:len(raw)-decimals], strings.TrimRight(raw[len(raw)-decimals:], "0")
	if frac == "" {
		return whole, nil
	}
	return whole + "." + frac, nil
}

// DecimalToAtomics parses a Flash decimal amount into atomic units, flooring digits
// beyond the mint's precision so a fill is never overstated.
func DecimalToAtomics(raw string, decimals int) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("flash: missing amount")
	}
	if decimals < 0 || decimals > 18 {
		return 0, fmt.Errorf("flash: unsupported decimals %d", decimals)
	}
	whole, frac, _ := strings.Cut(raw, ".")
	if whole == "" {
		whole = "0"
	}
	if !isDigits(whole) || (frac != "" && !isDigits(frac)) {
		return 0, fmt.Errorf("flash: invalid amount %q", raw)
	}
	if len(frac) > decimals {
		frac = frac[:decimals]
	}
	frac += strings.Repeat("0", decimals-len(frac))

	amount, err := strconv.ParseInt(strings.TrimLeft(whole+frac, "0"), 10, 64)
	if err != nil {
		if strings.TrimLeft(whole+frac, "0") == "" {
			return 0, nil
		}
		return 0, fmt.Errorf("flash: invalid amount %q: %w", raw, err)
	}
	return amount, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
