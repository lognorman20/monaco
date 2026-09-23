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

// AssetKind mirrors catalog kind for swap sizing (stock vs pre-IPO).
type AssetKind string

const (
	AssetKindStock  AssetKind = "stock"
	AssetKindPreIPO AssetKind = "pre_ipo"
)

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
	Kind           AssetKind
	TransferFeeBps int
	Amount         int64
	Wallet         Wallet
}

// Submission identifies a swap the provider has accepted for execution.
type Submission struct {
	// RequestID is the provider's id for the swap, persisted as execute_request_id.
	RequestID string
	// Receipt is provider-private state AwaitFill needs to poll (opaque to callers).
	Receipt string
	Request Request
	// QuotedOutputAmount is the Jupiter order outAmount for buys.
	QuotedOutputAmount int64
	// QuotedInputAmount is the Jupiter order inAmount for sells.
	QuotedInputAmount int64
}

// Fill is the terminal result of a swap. Amounts are atomic units of the input and output mints.
// Confirmed is true once the venue reports the swap landed, even if AwaitFill then
// fails to read the fill amounts; callers must not mark a confirmed swap as failed.
type Fill struct {
	Confirmed    bool
	Signature    string
	Status       string
	Code         int
	InputAmount  int64
	OutputAmount int64
	// QuotedOutputAmount is the Jupiter order outAmount (buy) before slippage lands.
	QuotedOutputAmount int64
	// QuotedInputAmount is the Jupiter order inAmount (sell) the venue quoted.
	QuotedInputAmount int64
}

// PollConfig controls how long AwaitFill polls. The zero value means provider default.
type PollConfig struct {
	MaxAttempts int
	Interval    time.Duration
}

// Provider submits treasury swaps to one venue and waits for their fills.
// Submit* covers quote, signing, and submission; AwaitFill polls to a terminal state.
type Provider interface {
	Name() string
	SubmitBuy(ctx context.Context, req Request) (Submission, error)
	SubmitSell(ctx context.Context, req Request) (Submission, error)
	AwaitFill(ctx context.Context, sub Submission, cfg PollConfig) (Fill, error)
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
