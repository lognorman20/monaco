package domain

import "fmt"

// WithdrawalStatus is persisted on withdrawals.status.
type WithdrawalStatus string

const (
	WithdrawalPending WithdrawalStatus = "pending"
	WithdrawalSettled WithdrawalStatus = "settled"
)

// ParseWithdrawalStatus parses a withdrawals.status column value.
func ParseWithdrawalStatus(raw string) (WithdrawalStatus, error) {
	switch WithdrawalStatus(raw) {
	case WithdrawalPending, WithdrawalSettled:
		return WithdrawalStatus(raw), nil
	default:
		return "", fmt.Errorf("invalid withdrawal status: %q", raw)
	}
}
