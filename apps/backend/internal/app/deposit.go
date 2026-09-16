package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DepositStatus is the lifecycle state of a deposit intent.
type DepositStatus string

const (
	DepositStatusPending   DepositStatus = "pending"
	DepositStatusConfirmed DepositStatus = "confirmed"
)

// Deposit is a user funding intent for a group treasury sweep.
type Deposit struct {
	ID           string
	UserID       string
	GroupID      string
	Amount       int64
	FromAddress  string
	Status       DepositStatus
	TxSignature  string
	CreatedAt    time.Time
}

// Position tracks a member's share units and net USDC in a group pot.
type Position struct {
	UserID           string
	GroupID          string
	ShareUnits       int64
	AmountDeposited  int64
	AmountWithdrawn  int64
}

// ObservedSweep is a confirmed on-chain USDC transfer from member wallet to group treasury.
type ObservedSweep struct {
	TxSignature   string
	FromAddress   string
	ToAddress     string
	Amount        int64
	DepositID     string
	UserID        string
	GroupID       string
}

// CreateDepositInput is the app-layer request to record a pending deposit.
type CreateDepositInput struct {
	UserID      string
	GroupID     string
	Amount      int64
	FromAddress string
}

// CreateDepositResult is the persisted pending deposit row.
type CreateDepositResult struct {
	Deposit Deposit
}

// ObserveSweepResult is the outcome after applying a confirmed treasury credit.
type ObserveSweepResult struct {
	Deposit  Deposit
	Position Position
	Credited bool
}

// ErrDepositNotFound means the deposit row does not exist.
var ErrDepositNotFound = errors.New("deposit not found")

// DepositService orchestrates deposit create and sweep credit flows.
type DepositService struct{}

// NewDepositService wires deposit dependencies.
func NewDepositService() *DepositService {
	return &DepositService{}
}

// CreateDeposit records a pending deposit intent. Implemented in M2-T3.
func (d *DepositService) CreateDeposit(_ context.Context, input CreateDepositInput) (CreateDepositResult, error) {
	if input.UserID == "" {
		return CreateDepositResult{}, fmt.Errorf("user id is required")
	}
	if input.GroupID == "" {
		return CreateDepositResult{}, fmt.Errorf("group id is required")
	}
	if input.Amount <= 0 {
		return CreateDepositResult{}, fmt.Errorf("amount must be positive")
	}
	if input.FromAddress == "" {
		return CreateDepositResult{}, fmt.Errorf("from address is required")
	}
	return CreateDepositResult{}, fmt.Errorf("CreateDeposit not implemented")
}

// ObserveSweep credits position share units on confirmed treasury arrival. Implemented in M2-T8.
func (d *DepositService) ObserveSweep(_ context.Context, sweep ObservedSweep) (ObserveSweepResult, error) {
	if sweep.TxSignature == "" {
		return ObserveSweepResult{}, fmt.Errorf("tx signature is required")
	}
	return ObserveSweepResult{}, fmt.Errorf("ObserveSweep not implemented")
}
