package worker

import "log/slog"

func logSweepAttempt(groupID, userID, depositID string, amount int64) {
	slog.Info("sweep attempt",
		"group_id", groupID,
		"user_id", userID,
		"deposit_id", depositID,
		"amount", amount,
	)
}

func logSweepConfirm(groupID, userID, depositID, txSignature string) {
	slog.Info("sweep confirmed",
		"group_id", groupID,
		"user_id", userID,
		"deposit_id", depositID,
		"tx_signature", txSignature,
	)
}
