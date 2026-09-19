package flash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// fakeFlashClient is the locked test double for provider and integration tests.
type fakeFlashClient struct {
	mu sync.Mutex

	quotes      map[string][]Quote
	quoteErrs   map[string]error
	submitErr   error
	orderPolls  map[string][]Order
	orderErrs   map[string]error
	submissions []SubmitOrderParams
	quoteCalls  int
}

// NewFakeClient returns an in-memory Flash client for tests.
func NewFakeClient() Client {
	return &fakeFlashClient{
		quotes:     make(map[string][]Quote),
		quoteErrs:  make(map[string]error),
		orderPolls: make(map[string][]Order),
		orderErrs:  make(map[string]error),
	}
}

func quoteKey(side, targetAsset, qty string) string {
	return fmt.Sprintf("%s:%s:%s", side, targetAsset, qty)
}

// FakeOrderID is the deterministic order id the fake returns for a submitted quote.
func FakeOrderID(quoteID string) string {
	sum := sha256.Sum256([]byte("flash-order:" + quoteID))
	return "ord-" + hex.EncodeToString(sum[:8])
}

// RegisterQuotes queues quotes for a side/target/qty. Each Quote call pops one; the last repeats.
func RegisterQuotes(client Client, side, targetAsset, qty string, quotes ...Quote) {
	fake := mustFake(client, "RegisterQuotes")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.quotes[quoteKey(side, targetAsset, qty)] = append([]Quote(nil), quotes...)
}

// RegisterQuoteError makes Quote fail for a side/target/qty.
func RegisterQuoteError(client Client, side, targetAsset, qty string, err error) {
	fake := mustFake(client, "RegisterQuoteError")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.quoteErrs[quoteKey(side, targetAsset, qty)] = err
}

// SetSubmitError makes every SubmitOrder fail.
func SetSubmitError(client Client, err error) {
	fake := mustFake(client, "SetSubmitError")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.submitErr = err
}

// RegisterOrderPoll queues GetOrder results for an order id. The last repeats.
func RegisterOrderPoll(client Client, orderID string, orders ...Order) {
	fake := mustFake(client, "RegisterOrderPoll")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.orderPolls[orderID] = append([]Order(nil), orders...)
}

// RegisterOrderError makes GetOrder fail for an order id.
func RegisterOrderError(client Client, orderID string, err error) {
	fake := mustFake(client, "RegisterOrderError")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.orderErrs[orderID] = err
}

// Submissions returns every order the fake accepted, in order.
func Submissions(client Client) []SubmitOrderParams {
	fake := mustFake(client, "Submissions")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]SubmitOrderParams(nil), fake.submissions...)
}

// QuoteCalls returns how many times Quote was called.
func QuoteCalls(client Client) int {
	fake := mustFake(client, "QuoteCalls")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.quoteCalls
}

func mustFake(client Client, caller string) *fakeFlashClient {
	fake, ok := client.(*fakeFlashClient)
	if !ok {
		panic("flash: " + caller + " requires NewFakeClient")
	}
	return fake
}

func (f *fakeFlashClient) Quote(ctx context.Context, params QuoteParams) (Quote, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quoteCalls++

	key := quoteKey(params.Side, params.TargetAsset, params.Qty)
	if err, ok := f.quoteErrs[key]; ok {
		return Quote{}, err
	}
	queue := f.quotes[key]
	if len(queue) == 0 {
		return Quote{}, fmt.Errorf("%w: no fake quote for %s", ErrNoRoute, key)
	}
	quote := queue[0]
	if len(queue) > 1 {
		f.quotes[key] = queue[1:]
	}
	return quote, nil
}

func (f *fakeFlashClient) SubmitOrder(ctx context.Context, params SubmitOrderParams) (string, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submitErr != nil {
		return "", f.submitErr
	}
	if params.QuoteID == "" || params.UserSignature == "" {
		return "", fmt.Errorf("flash: quoteId and userSignature are required")
	}
	f.submissions = append(f.submissions, params)
	return FakeOrderID(params.QuoteID), nil
}

func (f *fakeFlashClient) GetOrder(ctx context.Context, params GetOrderParams) (Order, error) {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.orderErrs[params.OrderID]; ok {
		return Order{}, err
	}
	queue := f.orderPolls[params.OrderID]
	if len(queue) == 0 {
		return Order{OrderID: params.OrderID, Status: OrderStatusPending}, nil
	}
	order := queue[0]
	if len(queue) > 1 {
		f.orderPolls[params.OrderID] = queue[1:]
	}
	order.OrderID = params.OrderID
	return order, nil
}
