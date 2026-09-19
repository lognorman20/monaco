package jupiter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// fakeJupiterClient is the locked test double for quote and integration tests.
type fakeJupiterClient struct {
	mu sync.Mutex

	quotes       map[string]BuyQuote
	quoteErrs    map[string]error
	orders       map[string]BuyOrder
	sellQuotes   map[string]SellQuote
	sellQuoteErr map[string]error
	executePolls  map[string][]ExecuteResult
	executeErrs   map[string]error
	lastSuccesses map[string]ExecuteResult
	quoteBuyCalls int

	settlementHooks map[string]func(ExecuteResult)
	settled         map[string]struct{}
}

// NewFakeClient returns an in-memory Jupiter client for tests.
func NewFakeClient() Client {
	return &fakeJupiterClient{
		quotes:       make(map[string]BuyQuote),
		quoteErrs:    make(map[string]error),
		orders:       make(map[string]BuyOrder),
		sellQuotes:   make(map[string]SellQuote),
		sellQuoteErr: make(map[string]error),
		executePolls:  make(map[string][]ExecuteResult),
		executeErrs:   make(map[string]error),
		lastSuccesses: make(map[string]ExecuteResult),

		settlementHooks: make(map[string]func(ExecuteResult)),
		settled:         make(map[string]struct{}),
	}
}

// RegisterSettlementHook runs fn once, when requestID first polls back as a confirmed success.
// The Jupiter and Privy fakes hold separate state, so tests use this to move the swap's proceeds
// into the treasury the way an on-chain fill would.
func RegisterSettlementHook(client Client, requestID string, fn func(ExecuteResult)) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterSettlementHook requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.settlementHooks[requestID] = fn
	fake.mu.Unlock()
}

// takeSettlementHook returns the pending settlement hook for a confirmed fill, once.
func (f *fakeJupiterClient) takeSettlementHook(requestID string) func(ExecuteResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, done := f.settled[requestID]; done {
		return nil
	}
	hook, ok := f.settlementHooks[requestID]
	if !ok {
		return nil
	}
	f.settled[requestID] = struct{}{}
	return hook
}

func quoteKey(outputMint string, usdcAmount int64) string {
	return fmt.Sprintf("%s:%d", outputMint, usdcAmount)
}

func sellQuoteKey(inputMint string, amount int64) string {
	return fmt.Sprintf("%s:%d", inputMint, amount)
}

// RegisterQuoteBuy configures a fake quote for outputMint and usdcAmount.
func RegisterQuoteBuy(client Client, outputMint string, usdcAmount int64, quote BuyQuote) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterQuoteBuy requires NewFakeClient")
	}
	fake.mu.Lock()
	key := quoteKey(outputMint, usdcAmount)
	fake.quotes[key] = quote
	delete(fake.quoteErrs, key)
	fake.mu.Unlock()
}

// RegisterQuoteBuyError forces QuoteBuy to return err for outputMint and usdcAmount.
func RegisterQuoteBuyError(client Client, outputMint string, usdcAmount int64, err error) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterQuoteBuyError requires NewFakeClient")
	}
	fake.mu.Lock()
	key := quoteKey(outputMint, usdcAmount)
	fake.quoteErrs[key] = err
	delete(fake.quotes, key)
	fake.mu.Unlock()
}

// RegisterBuyOrder configures an unsigned buy order for a request id.
func RegisterBuyOrder(client Client, requestID string, order BuyOrder) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterBuyOrder requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.orders[requestID] = order
	fake.mu.Unlock()
}

// RegisterExecutePoll configures poll responses for a request id.
func RegisterExecutePoll(client Client, requestID string, results []ExecuteResult) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterExecutePoll requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.executePolls[requestID] = results
	delete(fake.executeErrs, requestID)
	fake.mu.Unlock()
}

// RegisterExecuteError forces execute/poll to return err for requestID.
func RegisterExecuteError(client Client, requestID string, err error) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterExecuteError requires NewFakeClient")
	}
	fake.mu.Lock()
	fake.executeErrs[requestID] = err
	fake.mu.Unlock()
}

// RegisterSellQuote configures a fake sell quote.
func RegisterSellQuote(client Client, inputMint string, amount int64, quote SellQuote) {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		panic("jupiter: RegisterSellQuote requires NewFakeClient")
	}
	fake.mu.Lock()
	key := sellQuoteKey(inputMint, amount)
	fake.sellQuotes[key] = quote
	delete(fake.sellQuoteErr, key)
	fake.mu.Unlock()
}

// QuoteBuyCallCount returns how many QuoteBuy calls hit this fake client. Test hook.
func QuoteBuyCallCount(client Client) int {
	fake, ok := client.(*fakeJupiterClient)
	if !ok {
		return 0
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.quoteBuyCalls
}

func (f *fakeJupiterClient) QuoteBuy(ctx context.Context, params QuoteBuyParams) (BuyQuote, error) {
	f.mu.Lock()
	f.quoteBuyCalls++
	f.mu.Unlock()

	logQuoteAttempt(params.GroupID, params.UserID, params.Symbol, params.USDCAmount)

	if strings.TrimSpace(params.Taker) != "" {
		order, err := f.OrderBuy(ctx, OrderBuyParams{
			GroupID:    params.GroupID,
			UserID:     params.UserID,
			Symbol:     params.Symbol,
			OutputMint: params.OutputMint,
			Amount:     params.USDCAmount,
			Taker:      params.Taker,
		})
		if err != nil {
			logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
			return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
		}
		return BuyQuote{
			Routable:   true,
			InputMint:  order.InputMint,
			OutputMint: order.OutputMint,
			InAmount:   order.InAmount,
			OutAmount:  order.OutAmount,
			RequestID:  order.RequestID,
		}, nil
	}

	key := quoteKey(params.OutputMint, params.USDCAmount)
	f.mu.Lock()
	if err, ok := f.quoteErrs[key]; ok {
		f.mu.Unlock()
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, err
	}
	quote, ok := f.quotes[key]
	f.mu.Unlock()
	if !ok {
		reason := "no route"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return BuyQuote{Routable: false, InputMint: USDCMint, OutputMint: params.OutputMint}, ErrNoRoute
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
		return quote, ErrNoRoute
	}
	if quote.InputMint == "" {
		quote.InputMint = USDCMint
	}
	if quote.OutputMint == "" {
		quote.OutputMint = params.OutputMint
	}
	logQuoteSuccess(params.GroupID, params.UserID, params.Symbol, quote.RequestID, true)
	return quote, nil
}

func (f *fakeJupiterClient) OrderBuy(ctx context.Context, params OrderBuyParams) (BuyOrder, error) {
	logOrderAttempt(params.GroupID, params.UserID, params.Symbol, params.Amount)

	quote, err := f.QuoteBuy(ctx, QuoteBuyParams{
		GroupID:    params.GroupID,
		UserID:     params.UserID,
		Symbol:     params.Symbol,
		OutputMint: params.OutputMint,
		USDCAmount: params.Amount,
	})
	if err != nil {
		logOrderResult(params.GroupID, params.UserID, params.Symbol, "", err)
		return BuyOrder{}, err
	}
	requestID := quote.RequestID
	if requestID == "" {
		requestID = deterministicRequestID(params.OutputMint, params.Amount)
	}

	f.mu.Lock()
	if order, ok := f.orders[requestID]; ok {
		f.mu.Unlock()
		logOrderResult(params.GroupID, params.UserID, params.Symbol, order.RequestID, nil)
		return order, nil
	}
	f.mu.Unlock()

	order := BuyOrder{
		RequestID:   requestID,
		Transaction: deterministicUnsignedTx("buy", requestID),
		InAmount:    quote.InAmount,
		OutAmount:   quote.OutAmount,
		InputMint:   quote.InputMint,
		OutputMint:  quote.OutputMint,
	}
	logOrderResult(params.GroupID, params.UserID, params.Symbol, order.RequestID, nil)
	return order, nil
}

func (f *fakeJupiterClient) ExecuteBuy(ctx context.Context, params ExecuteBuyParams) (ExecuteResult, error) {
	logExecuteSubmit(params.GroupID, params.UserID, params.Symbol, "", params.RequestID)
	result := ExecuteResult{
		Status:    ExecuteStatusPending,
		Code:      -1,
		RequestID: params.RequestID,
	}
	logExecuteResult(params.GroupID, params.UserID, params.Symbol, params.RequestID, result.Status, 0, result.Code, nil, "", nil)
	return result, nil
}

func (f *fakeJupiterClient) PollExecute(ctx context.Context, params PollExecuteParams) (ExecuteResult, error) {
	return f.nextExecuteResult(ctx, params.GroupID, params.UserID, params.Symbol, params.RequestID)
}

func (f *fakeJupiterClient) nextExecuteResult(ctx context.Context, groupID, userID, symbol, requestID string) (ExecuteResult, error) {
	_ = ctx
	f.mu.Lock()
	if err, ok := f.executeErrs[requestID]; ok {
		f.mu.Unlock()
		return ExecuteResult{}, err
	}
	pollSeq, ok := f.executePolls[requestID]
	if !ok || len(pollSeq) == 0 {
		if cached, ok := f.lastSuccesses[requestID]; ok {
			f.mu.Unlock()
			cached.RequestID = requestID
			logPollTransition(groupID, userID, symbol, cached.Signature, ExecuteStatusPending, cached.Status, cached.Code)
			return cached, nil
		}
		f.mu.Unlock()
		return ExecuteResult{
			Status:    ExecuteStatusSuccess,
			Code:      0,
			Signature: deterministicTxSignature(requestID, "default"),
			RequestID: requestID,
		}, nil
	}
	result := pollSeq[0]
	if len(pollSeq) > 1 {
		f.executePolls[requestID] = pollSeq[1:]
	} else {
		delete(f.executePolls, requestID)
	}
	if result.IsConfirmedSuccess() {
		f.lastSuccesses[requestID] = result
	}
	f.mu.Unlock()

	result.RequestID = requestID
	if result.Signature == "" {
		result.Signature = deterministicTxSignature(requestID, result.Status)
	}
	if result.IsConfirmedSuccess() {
		if hook := f.takeSettlementHook(requestID); hook != nil {
			hook(result)
		}
	}
	logPollTransition(groupID, userID, symbol, result.Signature, ExecuteStatusPending, result.Status, result.Code)
	return result, nil
}

func (f *fakeJupiterClient) QuoteSell(ctx context.Context, params QuoteSellParams) (SellQuote, error) {
	logQuoteAttempt(params.GroupID, params.UserID, params.Symbol, params.Amount)

	key := sellQuoteKey(params.InputMint, params.Amount)
	f.mu.Lock()
	if err, ok := f.sellQuoteErr[key]; ok {
		f.mu.Unlock()
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, err.Error())
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, err
	}
	quote, ok := f.sellQuotes[key]
	f.mu.Unlock()
	if !ok {
		reason := "no route"
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, reason)
		return SellQuote{Routable: false, InputMint: params.InputMint, OutputMint: USDCMint}, ErrNoRoute
	}
	if !quote.Routable {
		logQuoteRefusal(params.GroupID, params.UserID, params.Symbol, "no route")
		return quote, ErrNoRoute
	}
	if quote.OutputMint == "" {
		quote.OutputMint = USDCMint
	}
	if quote.Transaction == "" {
		quote.Transaction = deterministicUnsignedTx("sell", quote.RequestID)
	}
	logQuoteSuccess(params.GroupID, params.UserID, params.Symbol, quote.RequestID, true)
	return quote, nil
}

func (f *fakeJupiterClient) SellToUSDC(ctx context.Context, params SellToUSDCParams) (ExecuteResult, error) {
	logExecuteSubmit(params.GroupID, params.UserID, params.Symbol, "", params.RequestID)
	result := ExecuteResult{
		Status:    ExecuteStatusPending,
		Code:      -1,
		RequestID: params.RequestID,
	}
	logExecuteResult(params.GroupID, params.UserID, params.Symbol, params.RequestID, result.Status, 0, result.Code, nil, "", nil)
	return result, nil
}

func deterministicRequestID(outputMint string, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("req:%s:%d", outputMint, amount)))
	return "req-" + hex.EncodeToString(sum[:8])
}

func deterministicUnsignedTx(kind, requestID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("unsigned:%s:%s", kind, requestID)))
	return "UNSIGNED" + hex.EncodeToString(sum[:16])
}

func deterministicTxSignature(requestID, status string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("sig:%s:%s", requestID, status)))
	return "SWAP" + hex.EncodeToString(sum[:16])
}
