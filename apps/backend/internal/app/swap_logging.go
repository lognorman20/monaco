package app

import "log/slog"

func logSwapQuoteAttempt(groupID, userID, symbol string, usdcAmount int64) {
	slog.Info("swap quote attempt",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"usdc_amount", usdcAmount,
	)
}

func logSwapExecuteSubmit(groupID, userID, symbol, txHash, executeRequestID string) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
	}
	if txHash != "" {
		args = append(args, "tx_signature", txHash)
	}
	if executeRequestID != "" {
		args = append(args, "execute_request_id", executeRequestID)
	}
	slog.Info("swap execute submit", args...)
}

func logSwapPollTransition(groupID, userID, symbol, txHash, fromStatus, toStatus string, code int) {
	args := []any{
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"from_status", fromStatus,
		"to_status", toStatus,
		"code", code,
	}
	if txHash != "" {
		args = append(args, "tx_signature", txHash)
	}
	slog.Info("swap poll transition", args...)
}

func logSwapRefusal(groupID, userID, symbol, reason string) {
	slog.Info("swap refused",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"reason", reason,
	)
}
