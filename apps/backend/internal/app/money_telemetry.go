package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// moneyOutcome classifies how a money operation ended for metrics. "rejected" is the
// caller's fault (bad input, paused agent, over budget) and is expected traffic; "error" is
// ours or an upstream's, and is what an error-rate alert should watch.
func moneyOutcome(err error) string {
	switch {
	case err == nil:
		return telemetry.OutcomeOK
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrAgentIntentRejected),
		errors.Is(err, ErrAgentPaused),
		errors.Is(err, ErrInvalidAgentAPIKey),
		errors.Is(err, ErrAgentGroupMismatch),
		errors.Is(err, ErrInvalidRedeemRequest),
		errors.Is(err, ErrInvalidPayoutProof),
		errors.Is(err, ErrRedeemAlreadyInProgress),
		errors.Is(err, ErrFakerGroupReadOnly):
		return telemetry.OutcomeRejected
	default:
		return telemetry.OutcomeError
	}
}

// recordSwap counts one treasury swap. created is false on an idempotent replay of a swap
// that already landed, which must not count its volume twice.
func recordSwap(event string, usdcMicros int64, created bool, err error) {
	if err == nil && created {
		telemetry.MoneyMoved(event, usdcMicros)
		return
	}
	if err == nil {
		telemetry.MoneyEvent(event, "replayed")
		return
	}
	telemetry.MoneyEvent(event, moneyOutcome(err))
}

// logAgentIntentOutcome is the service-layer record of every agent intent. It never logs
// the agent key: the intent id and cabal id identify the bot.
func logAgentIntentOutcome(ctx context.Context, in SubmitAgentIntentInput, result SubmitAgentIntentResult, err error) {
	attrs := []any{
		"group_id", in.GroupID,
		"side", string(in.Side),
		"symbol", in.Symbol,
		"usdc_micros", in.UsdcMicros,
		"token_amount", in.TokenAmount,
		"intent_id", result.IntentID,
		"status", result.Status,
		"transaction_id", result.TransactionID,
		"outcome", moneyOutcome(err),
	}
	switch moneyOutcome(err) {
	case telemetry.OutcomeOK:
		slog.InfoContext(ctx, "agent intent executed", attrs...)
	case telemetry.OutcomeRejected, "canceled":
		slog.WarnContext(ctx, "agent intent rejected", append(attrs, "reason", err.Error())...)
	default:
		slog.ErrorContext(ctx, "agent intent failed", append(attrs, "err", err)...)
	}
}
