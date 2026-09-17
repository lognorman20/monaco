package jupiter

import (
	"log/slog"

	"github.com/monaco/monaco/apps/backend/internal/logsnippet"
)

func logQuoteAttempt(groupID, userID, symbol string, usdcAmount int64) {
	slog.Info("jupiter quote attempt",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"usdc_amount", usdcAmount,
	)
}

func logQuoteRefusal(groupID, userID, symbol, reason string) {
	slog.Info("jupiter quote refused",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"reason", reason,
	)
}

func logExecuteSubmit(groupID, userID, symbol, txSignature, executeRequestID string) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
	}
	if txSignature != "" {
		args = append(args, "tx_signature", txSignature)
	}
	if executeRequestID != "" {
		args = append(args, "execute_request_id", executeRequestID)
	}
	slog.Info("jupiter execute submit", args...)
}

func logPollTransition(groupID, userID, symbol, txSignature, fromStatus, toStatus string, code int) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"from_status", fromStatus,
		"to_status", toStatus,
		"code", code,
	}
	if txSignature != "" {
		args = append(args, "tx_signature", txSignature)
	}
	slog.Info("jupiter poll transition", args...)
}

func logQuoteSuccess(groupID, userID, symbol, requestID string, routable bool) {
	slog.Info("jupiter quote result",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
		"routable", routable,
	)
}

func logOrderAttempt(groupID, userID, symbol string, amount int64) {
	slog.Info("jupiter order attempt",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"amount", amount,
	)
}

func logOrderResult(groupID, userID, symbol, requestID string, err error) {
	if err != nil {
		slog.Warn("jupiter order failed",
			"group_id", groupID,
			"user_id", userID,
			"symbol", symbol,
			"err", err,
		)
		return
	}
	slog.Info("jupiter order result",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
	)
}

func logOrderHTTPFailure(groupID, userID, symbol string, req buyOrderRequest, payer string, httpStatus int, body []byte, err error) {
	inputMint := req.InputMint
	if inputMint == "" {
		inputMint = USDCMint
	}
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"input_mint", inputMint,
		"output_mint", req.OutputMint,
		"amount", req.Amount,
	}
	if req.Taker != "" {
		args = append(args, "taker", req.Taker)
	}
	if payer != "" && payer != req.Taker {
		args = append(args, "payer", payer)
	}
	args = append(args, "swap_mode", "ExactIn", "slippage_bps", defaultSlippageBps)
	if httpStatus > 0 {
		args = append(args, "status", httpStatus)
	}
	if snippet := logsnippet.Body(body); snippet != "" {
		args = append(args, "response_body", snippet)
	}
	if err != nil {
		args = append(args, "err", err)
	}
	slog.Warn("jupiter order http failed", args...)
}

func logExecuteResult(groupID, userID, symbol, requestID, status string, httpStatus, code int, body []byte, jupiterErr string, err error) {
	if err != nil {
		args := []any{
			"group_id", groupID,
			"user_id", userID,
			"symbol", symbol,
			"request_id", requestID,
			"err", err,
		}
		if httpStatus > 0 {
			args = append(args, "status", httpStatus)
		}
		if snippet := logsnippet.Body(body); snippet != "" {
			args = append(args, "response_body", snippet)
		}
		slog.Warn("jupiter execute failed", args...)
		return
	}
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
		"status", status,
		"code", code,
	}
	if jupiterErr != "" {
		args = append(args, "jupiter_error", jupiterErr)
	}
	if status != ExecuteStatusSuccess || code != 0 {
		slog.Warn("jupiter execute result", args...)
		return
	}
	slog.Info("jupiter execute result", args...)
}

func logPollExhausted(groupID, userID, symbol, requestID, status string, code int, jupiterErr string) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
		"status", status,
		"code", code,
	}
	if jupiterErr != "" {
		args = append(args, "jupiter_error", jupiterErr)
	}
	slog.Warn("jupiter execute poll exhausted", args...)
}

func logPollTerminalFailure(groupID, userID, symbol, requestID, status string, code int, jupiterErr string) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
		"status", status,
		"code", code,
	}
	if jupiterErr != "" {
		args = append(args, "jupiter_error", jupiterErr)
	}
	slog.Warn("jupiter execute poll terminal failure", args...)
}
