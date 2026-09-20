// Package swapprovider is the seam between treasury swap orchestration and the
// venue that routes the swap (Jupiter, Definitive Flash). It holds only shared
// types so provider packages and internal/app can depend on it without cycles.
package swapprovider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Provider names accepted by SWAP_PROVIDER.
const (
	NameJupiter = "jupiter"
	NameFlash   = "flash"
)

// Side is the direction of a treasury swap.
type Side string

const (
	// SideBuy spends USDC for an xStock.
	SideBuy Side = "buy"
	// SideSell spends an xStock for USDC.
	SideSell Side = "sell"
)

// ErrNotRoutable means the provider has no executable route for the swap.
var ErrNotRoutable = errors.New("swap: not routable")

// Wallet is the treasury wallet that funds and signs the swap.
type Wallet struct {
	PrivyWalletID string
	SolanaAddress string
}

// Request describes one treasury swap. Amount is in atomic units of InputMint.
type Request struct {
	GroupID        string
	UserID         string
	Symbol         string
	Side           Side
	InputMint      string
	OutputMint     string
	InputDecimals  int
	OutputDecimals int
	Amount         int64
	Wallet         Wallet
}

// Prepared is a quoted and signed swap that has NOT been sent to the venue yet. Callers
// persist it before Submit so a crash or timeout after submit can still be resolved.
type Prepared struct {
	// RequestID is the venue id known before submit (Jupiter requestId, Flash quoteId).
	RequestID string
	// TxSignature and Blockhash identify the signed Solana transaction when the venue
	// broadcasts the transaction we signed. Empty when the venue settles with its own.
	TxSignature string
	Blockhash   string
	// ExpiresAt is when the venue stops accepting the signed payload. Zero when unknown.
	ExpiresAt time.Time
	// Payload is provider-private state Submit needs (opaque to callers).
	Payload string
	Request Request
}

// Submission identifies a swap the provider has accepted for execution.
type Submission struct {
	// RequestID is the provider's id for the swap, persisted as execute_request_id.
	RequestID string
	// Receipt is provider-private state AwaitFill needs to poll (opaque to callers).
	Receipt string
	Request Request
}

// Fill is the result of a swap. Amounts are atomic units of the input and output mints.
// Confirmed is true once the venue reports the swap landed, even if AwaitFill then
// fails to read the fill amounts. Rejected is true only when the venue guarantees no
// funds moved. Neither set means the outcome is unknown: callers must leave the swap
// pending for Resolve and must not retry it.
type Fill struct {
	Confirmed    bool
	Rejected     bool
	Signature    string
	Status       string
	Code         int
	InputAmount  int64
	OutputAmount int64
}

// PollConfig controls how long AwaitFill polls. The zero value means provider default.
type PollConfig struct {
	MaxAttempts int
	Interval    time.Duration
}

// ErrNotSubmitted wraps a Submit error when the venue definitively did not accept the swap.
// Any other Submit error leaves the outcome unknown.
var ErrNotSubmitted = errors.New("swap: not submitted")

// Outcome is the resolved state of a swap whose result was not observed inline.
type Outcome string

const (
	// OutcomeUnknown means the swap may still land; ask again later.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeFilled means funds moved and Fill carries the amounts.
	OutcomeFilled Outcome = "filled"
	// OutcomeFailed means the swap definitively did not and can no longer move funds.
	OutcomeFailed Outcome = "failed"
)

// PendingSwap is a persisted swap intent handed back to the provider for resolution.
type PendingSwap struct {
	Request Request
	// RequestID is the persisted execute_request_id: Submission.RequestID once Submitted,
	// otherwise Prepared.RequestID.
	RequestID   string
	Submitted   bool
	TxSignature string
	Blockhash   string
	ExpiresAt   time.Time
}

// Resolution is the provider's verdict on a PendingSwap.
type Resolution struct {
	Outcome Outcome
	Fill    Fill
	Reason  string
}

// Provider runs treasury swaps on one venue. Prepare* covers quote and signing and sends
// nothing; Submit hands the signed swap to the venue; AwaitFill polls to a terminal state;
// Resolve decides the outcome of a persisted swap after a crash, timeout, or poll error.
type Provider interface {
	Name() string
	PrepareBuy(ctx context.Context, req Request) (Prepared, error)
	PrepareSell(ctx context.Context, req Request) (Prepared, error)
	Submit(ctx context.Context, prepared Prepared) (Submission, error)
	AwaitFill(ctx context.Context, sub Submission, cfg PollConfig) (Fill, error)
	Resolve(ctx context.Context, pending PendingSwap) (Resolution, error)
}

// SignatureStatus is the chain's view of one transaction signature.
type SignatureStatus struct {
	// Found is false when no ledger entry exists for the signature.
	Found bool
	// Finalized is true once the transaction can no longer be rolled back.
	Finalized bool
	// Failed is true when the transaction landed but its instructions errored.
	Failed bool
	Err    string
}

// ChainReader answers, from Solana itself, whether a signed swap transaction landed.
type ChainReader interface {
	// SignatureStatus searches full ledger history for signature.
	SignatureStatus(ctx context.Context, signature string) (SignatureStatus, error)
	// IsBlockhashValid reports whether blockhash can still land as of the finalized bank.
	IsBlockhashValid(ctx context.Context, blockhash string) (bool, error)
	// TokenBalanceChanges returns post-minus-pre token balances per mint for owner in the
	// finalized transaction signature.
	TokenBalanceChanges(ctx context.Context, signature, owner string) (map[string]int64, error)
}

// TransactionSigner signs a base64 Solana transaction with the treasury wallet.
type TransactionSigner interface {
	SignTreasuryTransaction(ctx context.Context, walletID, unsignedTxBase64 string) (string, error)
}

// MessageSigner produces a raw 64-byte Ed25519 signature over message with the treasury wallet.
type MessageSigner interface {
	SignTreasuryMessage(ctx context.Context, walletID string, message []byte) ([]byte, error)
}

// StageError tags a submit failure with the step that failed, for branch logs.
type StageError struct {
	Stage     string
	RequestID string
	Err       error
}

func (e *StageError) Error() string {
	return e.Err.Error()
}

func (e *StageError) Unwrap() error {
	return e.Err
}

// AtStage wraps err with the failing stage. A nil err stays nil.
func AtStage(stage, requestID string, err error) error {
	if err == nil {
		return nil
	}
	return &StageError{Stage: stage, RequestID: requestID, Err: err}
}

// StageOf returns the stage and request id recorded on err, or fallback when untagged.
func StageOf(err error, fallback string) (stage, requestID string) {
	var staged *StageError
	if errors.As(err, &staged) {
		return staged.Stage, staged.RequestID
	}
	return fallback, ""
}

// ParseName validates a SWAP_PROVIDER value. Blank means Jupiter.
func ParseName(raw string) (string, error) {
	switch raw {
	case "", NameJupiter:
		return NameJupiter, nil
	case NameFlash:
		return NameFlash, nil
	default:
		return "", fmt.Errorf("unknown swap provider %q (want %s or %s)", raw, NameJupiter, NameFlash)
	}
}
