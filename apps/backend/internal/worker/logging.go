package worker

import (
	"log/slog"
	"time"
)

func logSweepPollerStarted(interval time.Duration) {
	slog.Info("sweep poller started", "interval", interval)
}

func logSweepPollerStopped() {
	slog.Info("sweep poller stopped")
}

func logSweepPollerTickStart(pendingCount int) {
	slog.Info("sweep poller tick start", "pending_count", pendingCount)
}

func logSweepPollerTickEnd(pendingCount int, err error) {
	args := []any{"pending_count", pendingCount}
	if err != nil {
		args = append(args, "err", err)
		slog.Error("sweep poller tick end", args...)
		return
	}
	slog.Info("sweep poller tick end", args...)
}

func logSweepPollerListPendingFailed(err error) {
	slog.Error("sweep poller list pending failed", "err", err)
}

func logSweepDepositProcessing(depositID, groupID, userID string, amount int64, fromAddress, status string, hasBroadcast bool) {
	slog.Info("sweep deposit processing",
		"deposit_id", depositID,
		"group_id", groupID,
		"user_id", userID,
		"amount", amount,
		"from_address", fromAddress,
		"status", status,
		"has_broadcast", hasBroadcast,
	)
}

func logSweepDepositSkipped(depositID, groupID, reason string, extra ...any) {
	args := []any{
		"deposit_id", depositID,
		"group_id", groupID,
		"reason", reason,
	}
	args = append(args, extra...)
	slog.Info("sweep deposit skipped", args...)
}

func logSweepDepositFailed(depositID, groupID, stage string, err error) {
	slog.Error("sweep deposit failed",
		"deposit_id", depositID,
		"group_id", groupID,
		"stage", stage,
		"err", err,
	)
}

func logSweepBalanceCheck(depositID, fromAddress string, balance, required int64, err error) {
	if err != nil {
		slog.Warn("sweep balance check failed",
			"deposit_id", depositID,
			"from_address", fromAddress,
			"required", required,
			"err", err,
		)
		return
	}
	slog.Info("sweep balance check",
		"deposit_id", depositID,
		"from_address", fromAddress,
		"balance", balance,
		"required", required,
	)
}

func logSweepAttempt(groupID, userID, depositID string, amount int64, fromAddress, treasuryAddress string) {
	slog.Info("sweep attempt",
		"group_id", groupID,
		"user_id", userID,
		"deposit_id", depositID,
		"amount", amount,
		"from_address", fromAddress,
		"treasury_address", treasuryAddress,
	)
}

func logSweepBroadcastSubmitted(depositID, groupID, txHash, treasuryAddress string) {
	slog.Info("sweep broadcast submitted",
		"deposit_id", depositID,
		"group_id", groupID,
		"tx_hash", txHash,
		"treasury_address", treasuryAddress,
	)
}

func logSweepDepositResuming(depositID, groupID, txHash string) {
	slog.Info("sweep deposit resuming broadcast",
		"deposit_id", depositID,
		"group_id", groupID,
		"tx_hash", txHash,
	)
}

func logSweepConfirmationCheck(depositID, txHash string, confirmed bool, err error) {
	if err != nil {
		slog.Warn("sweep confirmation check failed",
			"deposit_id", depositID,
			"tx_hash", txHash,
			"err", err,
		)
		return
	}
	slog.Info("sweep confirmation check",
		"deposit_id", depositID,
		"tx_hash", txHash,
		"confirmed", confirmed,
	)
}

func logSweepConfirm(groupID, userID, depositID, txHash string) {
	slog.Info("sweep confirmed",
		"group_id", groupID,
		"user_id", userID,
		"deposit_id", depositID,
		"tx_hash", txHash,
	)
}

func logSweepDepositCredited(depositID, groupID, userID, txHash string) {
	slog.Info("sweep deposit credited",
		"deposit_id", depositID,
		"group_id", groupID,
		"user_id", userID,
		"tx_hash", txHash,
	)
}

func logConfirmerConfirmationCheck(txHash string, confirmed bool, confirmationStatus string, err error) {
	if err != nil {
		slog.Warn("solana rpc confirmation check failed",
			"tx_hash", txHash,
			"err", err,
		)
		return
	}
	args := []any{
		"tx_hash", txHash,
		"confirmed", confirmed,
	}
	if confirmationStatus != "" {
		args = append(args, "confirmation_status", confirmationStatus)
	}
	slog.Info("solana rpc confirmation check", args...)
}
