package dex

import "math/big"

// BuyQuote is a test helper matching leftover Jupiter-shaped fixtures.
type BuyQuote struct {
	Routable    bool
	InputToken  string
	OutputToken string
	InAmount    string
	OutAmount   string
	RequestID   string
}

// SellQuote is a test helper for leftover Jupiter-shaped sell fixtures.
type SellQuote struct {
	Routable   bool
	InputToken string
	InAmount   string
	OutAmount  string
}

// BuyOrder is unused on Base; kept so old tests compile.
type BuyOrder struct {
	RequestID   string
	Transaction string
	InAmount    string
	OutAmount   string
	InputToken  string
	OutputToken string
}

// ExecuteResult is unused on Base; kept so old tests compile.
type ExecuteResult struct {
	Status             string
	Code               int
	Signature          string
	InputAmountResult  string
	OutputAmountResult string
}

const (
	ExecuteStatusPending = "pending"
	ExecuteStatusSuccess = "success"
	ExecuteStatusFailed  = "failed"
)

// TokenPrice is unused; kept for leftover RegisterPrice call sites.
type TokenPrice struct {
	ID    string
	Price string
}

// RegisterQuoteBuy adapts a legacy Jupiter buy quote onto dex.Client.
func RegisterQuoteBuy(client Client, tokenOut string, usdcIn int64, q BuyQuote) {
	amtIn := big.NewInt(usdcIn)
	amtOut := new(big.Int)
	if q.OutAmount != "" {
		amtOut, _ = amtOut.SetString(q.OutAmount, 10)
	}
	RegisterQuote(client, Quote{
		TokenIn:   USDCAddress(),
		TokenOut:  tokenOut,
		AmountIn:  amtIn,
		AmountOut: amtOut,
		Routable:  q.Routable,
	})
}

// RegisterSellQuote adapts a legacy Jupiter sell quote onto dex.Client.
func RegisterSellQuote(client Client, tokenIn string, amountIn int64, q SellQuote) {
	amtIn := big.NewInt(amountIn)
	amtOut := new(big.Int)
	if q.OutAmount != "" {
		amtOut, _ = amtOut.SetString(q.OutAmount, 10)
	}
	RegisterQuote(client, Quote{
		TokenIn:   tokenIn,
		TokenOut:  USDCAddress(),
		AmountIn:  amtIn,
		AmountOut: amtOut,
		Routable:  q.Routable,
	})
}

// RegisterBuyOrder is a no-op: Base swaps do not use a Jupiter order id.
func RegisterBuyOrder(client Client, requestID string, order BuyOrder) {
	_, _, _ = client, requestID, order
}

// RegisterExecutePoll is a no-op: confirmation is via evm receipts.
func RegisterExecutePoll(client Client, requestID string, results []ExecuteResult) {
	_, _, _ = client, requestID, results
}

// RegisterPrice is a no-op leftover from Jupiter Price API tests.
func RegisterPrice(unused any, token string, price TokenPrice) {
	_, _, _ = unused, token, price
}
