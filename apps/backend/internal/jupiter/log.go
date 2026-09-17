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

func logExecuteResult(groupID, userID, symbol, requestID, status string, code int, err error) {
	if err != nil {
		slog.Warn("jupiter execute failed",
			"group_id", groupID,
			"user_id", userID,
			"symbol", symbol,
			"request_id", requestID,
			"err", err,
		)
		return
	}
	slog.Info("jupiter execute result",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"request_id", requestID,
		"status", status,
		"code", code,
	)
}
