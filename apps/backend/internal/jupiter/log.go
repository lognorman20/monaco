package jupiter

import "log/slog"

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
