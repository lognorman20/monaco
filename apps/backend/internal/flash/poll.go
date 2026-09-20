package flash

import (
	"context"
	"fmt"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
)

// PollConfig controls Flash order and setup polling.
type PollConfig = swapprovider.PollConfig

// DefaultPollConfig is the standard poll loop for a Flash market order.
func DefaultPollConfig() PollConfig {
	return PollConfig{
		MaxAttempts: 30,
		Interval:    2 * time.Second,
	}
}

// TestPollConfig is a fast poll loop for tests using fake Flash clients.
func TestPollConfig() PollConfig {
	return PollConfig{
		MaxAttempts: 5,
		Interval:    time.Millisecond,
	}
}

// PollUntilFilled polls GET /orders/{orderId} until the order is filled with a
// settlement signature, or reaches a terminal unfilled status.
func PollUntilFilled(ctx context.Context, client Client, params GetOrderParams, cfg PollConfig) (Order, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Millisecond
	}

	last := Order{OrderID: params.OrderID, Status: OrderStatusPending}
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleep(ctx, cfg.Interval); err != nil {
				return last, err
			}
		}

		order, err := client.GetOrder(ctx, params)
		if err != nil {
			return last, err
		}
		if order.Status != last.Status {
			logPollTransition(params, last.Status, order.Status, order.TransactionID)
		}
		last = order

		// Fills can trail the status flip; keep polling until the signature is readable.
		if order.IsFilled() && order.TransactionID != "" {
			return order, nil
		}
		if order.IsTerminal() && !order.IsFilled() {
			logPollFailure(params, "terminal", order.Status, order.CloseReason)
			return order, fmt.Errorf("%w: status=%s reason=%s", ErrOrderRejected, order.Status, order.CloseReason)
		}
	}

	logPollFailure(params, "exhausted", last.Status, last.CloseReason)
	return last, fmt.Errorf("flash: order poll exhausted attempts status=%s", last.Status)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
