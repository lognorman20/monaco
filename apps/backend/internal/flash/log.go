package flash

import (
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/logsnippet"
)

func logQuoteAttempt(params QuoteParams) {
	slog.Info("flash quote attempt",
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"side", params.Side,
		"qty", params.Qty,
	)
}

func logQuoteResult(params QuoteParams, quoteID string, err error) {
	args := []any{
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"side", params.Side,
		"quote_id", quoteID,
	}
	if err != nil {
		slog.Warn("flash quote failed", append(args, "error", err.Error())...)
		return
	}
	slog.Info("flash quote result", args...)
}

func logSetupSubmit(params QuoteParams, instructionCount int, txSignature string, err error) {
	args := []any{
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"funder", params.FunderAddress,
		"instruction_count", instructionCount,
		"tx_signature", txSignature,
	}
	if err != nil {
		slog.Error("flash setup failed", append(args, "error", err.Error())...)
		return
	}
	slog.Info("flash setup submitted", args...)
}

func logOrderSubmit(params QuoteParams, quoteID, orderID string, err error) {
	args := []any{
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"side", params.Side,
		"quote_id", quoteID,
		"order_id", orderID,
	}
	if err != nil {
		slog.Error("flash order submit failed", append(args, "error", err.Error())...)
		return
	}
	slog.Info("flash order submit", args...)
}

func logPollTransition(params GetOrderParams, fromStatus, toStatus, txSignature string) {
	slog.Info("flash poll transition",
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"order_id", params.OrderID,
		"from_status", fromStatus,
		"to_status", toStatus,
		"tx_signature", txSignature,
	)
}

func logPollFailure(params GetOrderParams, reason, status, closeReason string) {
	slog.Error("flash poll failed",
		"group_id", params.GroupID,
		"user_id", params.UserID,
		"symbol", params.Symbol,
		"order_id", params.OrderID,
		"reason", reason,
		"status", status,
		"close_reason", closeReason,
	)
}

func logHTTPFailure(method, path string, status int, body []byte, err error) {
	slog.Error("flash http failed",
		"method", method,
		"path", path,
		"http_status", status,
		"body_snippet", logsnippet.Body(body),
		"error", err.Error(),
	)
}
