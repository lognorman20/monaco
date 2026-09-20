package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// DefaultSwapReconcileInterval is how often pending swaps are resolved.
const DefaultSwapReconcileInterval = 30 * time.Second

// PendingSwapReconciler resolves swaps whose outcome was not observed inline.
type PendingSwapReconciler interface {
	ReconcilePendingSwaps(ctx context.Context, now time.Time) (app.SwapReconcileSummary, error)
}

// SwapReconcilePoller settles pending treasury swaps left behind by a crash, a poll timeout, or
// a venue error after submit: confirmed on chain lands in the ledger, definitively failed frees
// the proposal to retry, anything else stays pending.
type SwapReconcilePoller struct {
	swaps PendingSwapReconciler
	clock Clock
}

// NewSwapReconcilePoller wires swap reconcile polling dependencies.
func NewSwapReconcilePoller(swaps PendingSwapReconciler, clock Clock) *SwapReconcilePoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &SwapReconcilePoller{swaps: swaps, clock: clock}
}

// RunSwapReconcilePoller ticks until ctx is cancelled. It reconciles once immediately so swaps
// interrupted by the previous shutdown are picked up at boot.
func RunSwapReconcilePoller(ctx context.Context, poller *SwapReconcilePoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSwapReconcileInterval
	}

	slog.Info("swap reconcile poller started", "interval", interval)
	telemetry.RegisterPoller(PollerSwapReconcile, interval)
	defer slog.Info("swap reconcile poller stopped")

	telemetry.GuardTick(ctx, PollerSwapReconcile, func() error { return poller.tick(ctx) })

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			telemetry.GuardTick(ctx, PollerSwapReconcile, func() error { return poller.tick(ctx) })
		}
	}
}

func (p *SwapReconcilePoller) tick(ctx context.Context) error {
	if p == nil || p.swaps == nil {
		return nil
	}
	summary, err := p.swaps.ReconcilePendingSwaps(ctx, p.clock.Now())
	if err != nil {
		slog.Error("swap reconcile poller tick failed", "err", err)
		return err
	}
	if summary == (app.SwapReconcileSummary{}) {
		return nil
	}
	args := []any{
		"confirmed", summary.Confirmed,
		"failed", summary.Failed,
		"unknown", summary.Unknown,
		"errors", summary.Errors,
	}
	if summary.Errors > 0 {
		slog.Error("swap reconcile poller tick end", args...)
		return fmt.Errorf("swap reconcile: %d of %d pending swaps could not be checked",
			summary.Errors, summary.Errors+summary.Confirmed+summary.Failed+summary.Unknown)
	}
	slog.Info("swap reconcile poller tick end", args...)
	return nil
}
