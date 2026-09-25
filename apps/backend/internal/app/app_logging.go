package app

import "log/slog"

// --- held assets ---

// logHeldScanFailed records a cabal that could not be scanned for GET
// /v1/assets/held. The aggregate carries on without it: one unreachable cabal
// must not turn "In your cabals" into "you own nothing".
func logHeldScanFailed(groupID string, err error) {
	slog.Warn("held assets scan failed for group",
		"group_id", groupID,
		"err", err,
	)
}

// --- deposit ---

func logDepositCreateStart(groupID string, amount int64) {
	slog.Info("deposit create start",
		"group_id", groupID,
		"amount", amount,
	)
}

func logDepositCreateSuccess(userID, groupID, depositID string, amount int64) {
	slog.Info("deposit create success",
		"user_id", userID,
		"group_id", groupID,
		"deposit_id", depositID,
		"amount", amount,
	)
}

func logDepositObserveSweepStart(depositID, groupID, userID string, amount int64, txSignature string) {
	slog.Info("deposit observe sweep start",
		"deposit_id", depositID,
		"group_id", groupID,
		"user_id", userID,
		"amount", amount,
		"tx_signature", txSignature,
	)
}

func logDepositObserveSweepIdempotent(depositID, txSignature string) {
	slog.Info("deposit observe sweep idempotent",
		"deposit_id", depositID,
		"tx_signature", txSignature,
	)
}

func logDepositObserveSweepConfirmed(depositID, groupID, userID, txSignature string, credited bool) {
	slog.Info("deposit observe sweep confirmed",
		"deposit_id", depositID,
		"group_id", groupID,
		"user_id", userID,
		"tx_signature", txSignature,
		"credited", credited,
	)
}

func logDepositBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logDepositBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- redeem ---

func logRedeemStart(groupID, userID, resumeJobID string) {
	args := []any{"group_id", groupID, "user_id", userID}
	if resumeJobID != "" {
		args = append(args, "resume_job_id", resumeJobID)
	}
	slog.Info("redeem start", args...)
}

func logRedeemLockContended(userID, groupID string) {
	slog.Warn("redeem lock contended",
		"user_id", userID,
		"group_id", groupID,
	)
}

func logRedeemDebited(jobID, userID, groupID string, shareUnits, sliceUsdc int64) {
	slog.Info("redeem debited",
		"job_id", jobID,
		"user_id", userID,
		"group_id", groupID,
		"share_units", shareUnits,
		"slice_usdc", sliceUsdc,
	)
}

func logRedeemStatusTransition(jobID, fromStatus, toStatus string) {
	slog.Info("redeem status transition",
		"job_id", jobID,
		"from_status", fromStatus,
		"to_status", toStatus,
	)
}

func logRedeemSettled(jobID, userID, groupID, withdrawalID string, sliceUsdc int64) {
	slog.Info("redeem settled",
		"job_id", jobID,
		"user_id", userID,
		"group_id", groupID,
		"withdrawal_id", withdrawalID,
		"slice_usdc", sliceUsdc,
	)
}

func logRedeemBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logRedeemBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- governance ---

func logGovernanceCreateGroupStart(userID, name string) {
	slog.Info("governance create group start",
		"user_id", userID,
		"name", name,
	)
}

func logGovernanceCreateGroupSuccess(groupID, userID, name string) {
	slog.Info("governance create group success",
		"group_id", groupID,
		"user_id", userID,
		"name", name,
	)
}

func logGovernanceJoinGroupStart(userID, groupID string) {
	slog.Info("governance join group start",
		"user_id", userID,
		"group_id", groupID,
	)
}

func logGovernanceJoinGroupAlreadyMember(userID, groupID string) {
	slog.Info("governance join group already member",
		"user_id", userID,
		"group_id", groupID,
	)
}

func logGovernanceJoinGroupSuccess(userID, groupID string) {
	slog.Info("governance join group success",
		"user_id", userID,
		"group_id", groupID,
	)
}

func logGovernanceLeaveGroupStart(userID, groupID string) {
	slog.Info("governance leave group start", "user_id", userID, "group_id", groupID)
}

func logGovernanceLeaveGroupSuccess(userID, groupID string, wasCreator bool) {
	slog.Info("governance leave group success", "user_id", userID, "group_id", groupID, "was_creator", wasCreator)
}

func logGovernanceCreateProposalStart(groupID, proposerID, symbol string, usdcMicros int64) {
	slog.Info("governance create proposal start",
		"group_id", groupID,
		"proposer_id", proposerID,
		"symbol", symbol,
		"usdc_micros", usdcMicros,
	)
}

func logGovernanceCreateProposalSuccess(proposalID, groupID, proposerID string) {
	slog.Info("governance create proposal success",
		"proposal_id", proposalID,
		"group_id", groupID,
		"proposer_id", proposerID,
	)
}

func logGovernanceCastVoteStart(proposalID, voterID, choice string) {
	slog.Info("governance cast vote start",
		"proposal_id", proposalID,
		"voter_id", voterID,
		"choice", choice,
	)
}

func logGovernanceCastVoteIdempotent(proposalID, voterID string) {
	slog.Info("governance cast vote idempotent",
		"proposal_id", proposalID,
		"voter_id", voterID,
	)
}

func logGovernanceProposalStatusTransition(proposalID string, fromStatus, toStatus ProposalStatus) {
	slog.Info("governance proposal status transition",
		"proposal_id", proposalID,
		"from_status", fromStatus,
		"to_status", toStatus,
	)
}

func logGovernanceBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logGovernanceBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- session ---

func logSessionOpenStart() {
	slog.Info("session open start")
}

func logSessionOpenSuccess(userID string) {
	slog.Info("session open success",
		"user_id", userID,
	)
}

func logSessionGetMeStart() {
	slog.Info("session get me start")
}

func logSessionGetMeSuccess(userID string) {
	slog.Info("session get me success",
		"user_id", userID,
	)
}

func logSessionEnsureWalletExisting(userID string) {
	slog.Info("session ensure wallet existing",
		"user_id", userID,
	)
}

func logSessionEnsureWalletCreated(userID string) {
	slog.Info("session ensure wallet created",
		"user_id", userID,
	)
}

func logSessionBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logSessionBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- home ---

func logHomeGetStart() {
	slog.Info("home get start")
}

func logHomeGetSuccess(userID string, groupCount, peopleCount int) {
	slog.Info("home get success",
		"user_id", userID,
		"group_count", groupCount,
		"people_count", peopleCount,
	)
}

func logHomeBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- group ---

func logGroupCreateStart(userID, name string) {
	slog.Info("group create start",
		"user_id", userID,
		"name", name,
	)
}

func logGroupCreateSuccess(groupID, userID, name string) {
	slog.Info("group create success",
		"group_id", groupID,
		"user_id", userID,
		"name", name,
	)
}

func logGroupGetStart(userID, groupID string) {
	slog.Info("group get start",
		"user_id", userID,
		"group_id", groupID,
	)
}

func logGroupGetSuccess(groupID string) {
	slog.Info("group get success",
		"group_id", groupID,
	)
}

func logGroupBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logGroupBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- buy / execute ---

func logExecuteOnPassStart(proposalID, groupID, symbol string, usdcMicros int64) {
	slog.Info("execute on pass start",
		"proposal_id", proposalID,
		"group_id", groupID,
		"symbol", symbol,
		"usdc_micros", usdcMicros,
	)
}

func logExecuteOnPassIdempotent(proposalID, txID string) {
	slog.Info("execute on pass idempotent",
		"proposal_id", proposalID,
		"transaction_id", txID,
	)
}

func logExecuteOnPassSuccess(proposalID, txID string, created bool) {
	slog.Info("execute on pass success",
		"proposal_id", proposalID,
		"transaction_id", txID,
		"created", created,
	)
}

func logExecuteOnPassBranchWarn(msg, reason string, attrs ...any) {
	args := append([]any{"reason", reason}, attrs...)
	slog.Warn(msg, args...)
}

func logExecuteOnPassBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- swap (extends swap_logging.go) ---

func logSwapBuyStart(groupID, userID, symbol string, usdcAmount int64) {
	slog.Info("swap buy start",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"usdc_amount", usdcAmount,
	)
}

func logSwapBuySuccess(groupID, userID, symbol, txID string, created bool) {
	slog.Info("swap buy success",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"transaction_id", txID,
		"created", created,
	)
}

func logSwapSellStart(groupID, userID, symbol string, amount int64) {
	slog.Info("swap sell start",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"amount", amount,
	)
}

func logSwapSellSuccess(groupID, userID, symbol, txID string, created bool) {
	slog.Info("swap sell success",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"transaction_id", txID,
		"created", created,
	)
}

func logSwapSellFill(groupID, userID, symbol string, sentIn, quotedIn, fillInput int64) {
	slog.Info("swap sell fill",
		"group_id", groupID,
		"user_id", userID,
		"symbol", symbol,
		"sent_in", sentIn,
		"quoted_in", quotedIn,
		"fill_input", fillInput,
	)
}

func logSwapBranchError(msg string, err error, attrs ...any) {
	args := append([]any{"err", err}, attrs...)
	slog.Error(msg, args...)
}

// --- agent intents ---

// logAgentIntentStatusWriteFailed records an intent whose final status could not be saved,
// so the audit trail still says "accepted". The next intent of the same agent repairs it
// from the ledger; this line is what to alert on until then.
func logAgentIntentStatusWriteFailed(intentID, status, transactionID string, err error) {
	slog.Error("agent intent status write failed",
		"intent_id", intentID,
		"status", status,
		"transaction_id", transactionID,
		"err", err,
	)
}
