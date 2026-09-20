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

func logSweepPollerTickEnd(handledCount int, err error) {
	args := []any{"handled_count", handledCount}
	if err != nil {
		args = append(args, "err", err)
		slog.Error("sweep poller tick end", args...)
		return
	}
	slog.Info("sweep poller tick end", args...)
}

func logSweepPollerClaimFailed(err error) {
	slog.Error("sweep poller claim failed", "err", err)
}

func logSweepDepositBackoff(depositID, groupID, stage string, attempts int, nextAttemptAt time.Time) {
	slog.Warn("sweep deposit backing off",
		"deposit_id", depositID,
		"group_id", groupID,
		"stage", stage,
		"attempts", attempts,
		"next_attempt_at", nextAttemptAt.UTC(),
	)
}

func logSweepDropped(depositID, groupID, txSignature string, finalizedHeight uint64, lastValidBlockHeight int64, cleared bool) {
	slog.Warn("sweep dropped before landing; deposit eligible for re-submit",
		"deposit_id", depositID,
		"group_id", groupID,
		"tx_signature", txSignature,
		"finalized_block_height", finalizedHeight,
		"last_valid_block_height", lastValidBlockHeight,
		"cleared", cleared,
	)
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

func logSweepBroadcastSubmitted(depositID, groupID, txSignature, treasuryAddress string) {
	slog.Info("sweep broadcast submitted",
		"deposit_id", depositID,
		"group_id", groupID,
		"tx_signature", txSignature,
		"treasury_address", treasuryAddress,
	)
}

func logSweepDepositResuming(depositID, groupID, txSignature string) {
	slog.Info("sweep deposit resuming broadcast",
		"deposit_id", depositID,
		"group_id", groupID,
		"tx_signature", txSignature,
	)
}

func logSweepConfirmationCheck(depositID, txSignature string, confirmed bool, err error) {
	if err != nil {
		slog.Warn("sweep confirmation check failed",
			"deposit_id", depositID,
			"tx_signature", txSignature,
			"err", err,
		)
		return
	}
	slog.Info("sweep confirmation check",
		"deposit_id", depositID,
		"tx_signature", txSignature,
		"confirmed", confirmed,
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

func logSweepDepositCredited(depositID, groupID, userID, txSignature string) {
	slog.Info("sweep deposit credited",
		"deposit_id", depositID,
		"group_id", groupID,
		"user_id", userID,
		"tx_signature", txSignature,
	)
}

func logSolanaRPCConfirmationCheck(txSignature string, confirmed bool, confirmationStatus string, err error) {
	if err != nil {
		slog.Warn("solana rpc confirmation check failed",
			"tx_signature", txSignature,
			"err", err,
		)
		return
	}
	args := []any{
		"tx_signature", txSignature,
		"confirmed", confirmed,
	}
	if confirmationStatus != "" {
		args = append(args, "confirmation_status", confirmationStatus)
	}
	slog.Info("solana rpc confirmation check", args...)
}
