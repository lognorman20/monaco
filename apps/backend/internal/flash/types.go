// Package flash routes treasury swaps through the Definitive Flash API
// (https://flash.definitive.fi/docs). On Solana an order is a quote, an optional
// one-time onchain setup (token accounts + SPL delegation to the Flash program),
// an offchain Ed25519 signature over the quote's order message, and a submit.
package flash

import "errors"

// Order statuses returned by GET /orders/{orderId}.
const (
	OrderStatusPending         = "ORDER_STATUS_PENDING"
	OrderStatusAccepted        = "ORDER_STATUS_ACCEPTED"
	OrderStatusPartiallyFilled = "ORDER_STATUS_PARTIALLY_FILLED"
	OrderStatusFilled          = "ORDER_STATUS_FILLED"
	OrderStatusCancelled       = "ORDER_STATUS_CANCELLED"
	OrderStatusRejected        = "ORDER_STATUS_REJECTED"
	OrderStatusTerminated      = "ORDER_STATUS_TERMINATED"
)

const (
	chainSolana     = "solana"
	orderTypeMarket = "market"
)

// ErrNoRoute means Flash cannot price or route the requested pair.
var ErrNoRoute = errors.New("flash: no route")

// ErrUnauthorized means Flash rejected the API key.
var ErrUnauthorized = errors.New("flash: unauthorized")

// ErrQuoteExpired means the quote's signing deadline passed before submit.
var ErrQuoteExpired = errors.New("flash: quote expired")

// ErrOrderRejected means Flash closed the order without filling it.
var ErrOrderRejected = errors.New("flash: order rejected")

// ErrSetupNotConfirmed means the onchain setup did not land before the retry budget ran out.
var ErrSetupNotConfirmed = errors.New("flash: setup not confirmed")

// ErrOrderMessageMismatch means the message Flash asked us to sign does not commit
// to the mint and amount we requested. The treasury never signs such a message.
var ErrOrderMessageMismatch = errors.New("flash: order message mismatch")

// AccountMeta is one account of a Solana instruction returned by a quote.
type AccountMeta struct {
	Pubkey     string `json:"pubkey"`
	IsSigner   bool   `json:"isSigner"`
	IsWritable bool   `json:"isWritable"`
}

// Instruction is a Solana instruction returned by a quote. Data is base58.
type Instruction struct {
	ProgramID string        `json:"programId"`
	Accounts  []AccountMeta `json:"accounts"`
	Data      string        `json:"data"`
}

// QuoteParams requests a same-chain Solana market quote. Qty is a decimal string in
// the spent asset's units (contra on buy, target on sell).
type QuoteParams struct {
	GroupID       string
	UserID        string
	Symbol        string
	Side          string
	TargetAsset   string
	ContraAsset   string
	Qty           string
	MaxSlippage   string
	FunderAddress string
}

// QuoteLeg is the spent or received side of a quote.
type QuoteLeg struct {
	Asset    string `json:"asset"`
	Amount   string `json:"amount"`
	Notional string `json:"notional"`
}

// Quote is a parsed Flash quote. The Svm* fields are empty when no funder was sent.
type Quote struct {
	QuoteID             string
	From                QuoteLeg
	To                  QuoteLeg
	OrderMessage        string
	Nonce               string
	Deadline            string
	ATASetupIxs         []Instruction
	DelegateIx          *Instruction
	SponsoredDelegateTx string
}

// NeedsOnchainSetup reports whether instructions must land before the order is submitted.
func (q Quote) NeedsOnchainSetup() bool {
	return len(q.ATASetupIxs) > 0 || q.DelegateIx != nil
}

// SubmitOrderParams submits a signed quote. UserSignature is base58.
type SubmitOrderParams struct {
	Quote                     QuoteParams
	QuoteID                   string
	UserSignature             string
	Nonce                     string
	Deadline                  string
	SignedSponsoredDelegateTx string
}

// GetOrderParams identifies an order to poll.
type GetOrderParams struct {
	GroupID       string
	UserID        string
	Symbol        string
	OrderID       string
	FunderAddress string
}

// Order is a parsed Flash order with its fill totals (decimal strings, normalized units).
type Order struct {
	OrderID            string
	Status             string
	CloseReason        string
	FilledTargetAmount string
	FilledContraAmount string
	TransactionID      string
}

// IsFilled reports whether Flash fully filled the order.
func (o Order) IsFilled() bool {
	return o.Status == OrderStatusFilled
}

// IsTerminal reports whether polling should stop.
func (o Order) IsTerminal() bool {
	switch o.Status {
	case OrderStatusFilled, OrderStatusCancelled, OrderStatusRejected, OrderStatusTerminated:
		return true
	default:
		return false
	}
}
