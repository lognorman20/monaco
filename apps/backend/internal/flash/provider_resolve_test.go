package flash

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// submitBuy prepares and submits in one step, the way the swap service does between
// persisting the intent and polling the fill.
func submitBuy(provider *SwapProvider, req swapprovider.Request) (swapprovider.Submission, error) {
	prepared, err := provider.PrepareBuy(context.Background(), req)
	if err != nil {
		return swapprovider.Submission{}, err
	}
	return provider.Submit(context.Background(), prepared)
}

func submitSell(provider *SwapProvider, req swapprovider.Request) (swapprovider.Submission, error) {
	prepared, err := provider.PrepareSell(context.Background(), req)
	if err != nil {
		return swapprovider.Submission{}, err
	}
	return provider.Submit(context.Background(), prepared)
}

func TestFlashProvider_prepare_sendsNoOrderAndCarriesQuoteIdentity(t *testing.T) {
	t.Parallel()

	client := NewFakeClient()
	RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-prepare", testUSDCMint, 5_000_000))
	provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

	prepared, err := provider.PrepareBuy(context.Background(), testBuyRequest())

	if err != nil {
		t.Fatalf("PrepareBuy() error = %v", err)
	}
	if got := len(Submissions(client)); got != 0 {
		t.Fatalf("PrepareBuy() submitted %d orders, want 0 before the intent is persisted", got)
	}
	if prepared.RequestID != "quote-prepare" {
		t.Fatalf("RequestID = %q, want the quote id", prepared.RequestID)
	}
	if prepared.ExpiresAt.IsZero() || prepared.ExpiresAt.Location() != time.UTC {
		t.Fatalf("ExpiresAt = %v, want the quote deadline in UTC", prepared.ExpiresAt)
	}

	sub, err := provider.Submit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if sub.RequestID != FakeOrderID("quote-prepare") || len(Submissions(client)) != 1 {
		t.Fatalf("Submit() = %+v with %d submissions, want one order", sub, len(Submissions(client)))
	}
}

func TestFlashProvider_submit_onlyExplicitRefusalIsNotSubmitted(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err          error
		notSubmitted bool
	}{
		"4xx refusal":    {&APIError{Status: http.StatusBadRequest, Code: "BAD", Message: "quote expired"}, true},
		"unauthorized":   {ErrUnauthorized, true},
		"5xx":            {&APIError{Status: http.StatusBadGateway, Message: "upstream"}, false},
		"network":        {errors.New("read tcp: connection reset by peer"), false},
		"client timeout": {context.DeadlineExceeded, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := NewFakeClient()
			RegisterQuotes(client, "buy", testAAPLxMint, "5", readyQuote("quote-"+name, testUSDCMint, 5_000_000))
			SetSubmitError(client, tc.err)
			provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

			_, err := submitBuy(provider, testBuyRequest())

			if err == nil {
				t.Fatal("Submit() error = nil, want an error")
			}
			if got := errors.Is(err, swapprovider.ErrNotSubmitted); got != tc.notSubmitted {
				t.Fatalf("errors.Is(err, ErrNotSubmitted) = %v, want %v (err = %v)", got, tc.notSubmitted, err)
			}
		})
	}
}

func TestFlashProvider_resolve(t *testing.T) {
	t.Parallel()

	pendingOrder := func(orderID string) swapprovider.PendingSwap {
		return swapprovider.PendingSwap{Request: testBuyRequest(), RequestID: orderID, Submitted: true}
	}

	t.Run("filled order reports amounts", func(t *testing.T) {
		client := NewFakeClient()
		RegisterOrderPoll(client, "ord-filled",
			Order{Status: OrderStatusFilled, TransactionID: "flash-sig", FilledTargetAmount: "0.01486083", FilledContraAmount: "5"})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), pendingOrder("ord-filled"))

		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if got.Outcome != swapprovider.OutcomeFilled || got.Fill.Signature != "flash-sig" ||
			got.Fill.InputAmount != 5_000_000 || got.Fill.OutputAmount != 1_486_083 {
			t.Fatalf("Resolve() = %+v, want filled 5000000/1486083", got)
		}
	})

	t.Run("closed without fills is failed", func(t *testing.T) {
		client := NewFakeClient()
		RegisterOrderPoll(client, "ord-closed", Order{Status: OrderStatusRejected, CloseReason: "SLIPPAGE"})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), pendingOrder("ord-closed"))

		if err != nil || got.Outcome != swapprovider.OutcomeFailed {
			t.Fatalf("Resolve() = %+v, %v, want failed", got, err)
		}
	})

	t.Run("terminated after a partial fill is never failed", func(t *testing.T) {
		client := NewFakeClient()
		RegisterOrderPoll(client, "ord-partial",
			Order{Status: OrderStatusTerminated, FilledTargetAmount: "0.004", FilledContraAmount: "1.5"})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), pendingOrder("ord-partial"))

		if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
			t.Fatalf("Resolve() = %+v, %v, want unknown: funds moved", got, err)
		}
	})

	t.Run("open order stays unknown", func(t *testing.T) {
		client := NewFakeClient()
		RegisterOrderPoll(client, "ord-open", Order{Status: OrderStatusAccepted})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), pendingOrder("ord-open"))

		if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
			t.Fatalf("Resolve() = %+v, %v, want unknown", got, err)
		}
	})

	t.Run("lookup error is returned, not read as failed", func(t *testing.T) {
		client := NewFakeClient()
		RegisterOrderError(client, "ord-err", &APIError{Status: http.StatusBadGateway, Message: "upstream"})
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), pendingOrder("ord-err"))

		if err == nil || got.Outcome == swapprovider.OutcomeFailed {
			t.Fatalf("Resolve() = %+v, %v, want an error and no verdict", got, err)
		}
	})

	t.Run("order id never recorded stays unknown even after the deadline", func(t *testing.T) {
		client := NewFakeClient()
		provider := newTestProvider(client, newRecordingSigner(), &recordingSetup{})

		got, err := provider.Resolve(context.Background(), swapprovider.PendingSwap{
			Request:   testBuyRequest(),
			RequestID: "quote-orphan",
			ExpiresAt: testClock.Add(-time.Hour),
		})

		if err != nil || got.Outcome != swapprovider.OutcomeUnknown {
			t.Fatalf("Resolve() = %+v, %v, want unknown", got, err)
		}
	})
}
