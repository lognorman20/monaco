package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/privy"
	"github.com/monaco/monaco/apps/backend/internal/swapprovider"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const (
	// SwapReconcileMinAge keeps the reconciler off swaps an executor is still polling inline,
	// and gives lagging RPC nodes time to see a transaction before its expiry is trusted.
	SwapReconcileMinAge = 2 * time.Minute

	// swapUnresolvedAlertAge is when a still-unknown swap stops being routine and needs a human.
	swapUnresolvedAlertAge = 30 * time.Minute

	swapReconcileBatch = 100

	proposalExecuteBaseBackoff = 15 * time.Second
	proposalExecuteMaxBackoff  = 15 * time.Minute
)

// ProposalExecuteBackoff is the wait after the attempts-th failed execute of a proposal.
func ProposalExecuteBackoff(attempts int) time.Duration {
	delay := proposalExecuteBaseBackoff
	for i := 1; i < attempts && delay < proposalExecuteMaxBackoff; i++ {
		delay *= 2
	}
	if delay > proposalExecuteMaxBackoff {
		delay = proposalExecuteMaxBackoff
	}
	return delay
}

// SwapReconcileSummary counts what one reconcile pass decided.
type SwapReconcileSummary struct {
	Confirmed int
	Failed    int
	Unknown   int
	Errors    int
}

// ReconcilePendingSwaps resolves every pending swap old enough that no executor is still
// polling it. It is safe to run on several instances at once: confirming and failing a row
// are both conditional on it still being pending.
func (s *SwapService) ReconcilePendingSwaps(ctx context.Context, now time.Time) (SwapReconcileSummary, error) {
	var summary SwapReconcileSummary
	pending, err := s.store.ListPendingSwaps(ctx, now.Add(-SwapReconcileMinAge), swapReconcileBatch)
	if err != nil {
		return summary, err
	}
	for _, row := range pending {
		outcome, err := s.reconcilePendingSwap(ctx, row, now)
		switch {
		case err != nil:
			summary.Errors++
		case outcome == swapprovider.OutcomeFilled:
			summary.Confirmed++
		case outcome == swapprovider.OutcomeFailed:
			summary.Failed++
		default:
			summary.Unknown++
		}
	}
	return summary, nil
}

// ReconcilePendingSwap resolves one pending swap by transaction id, whatever its age.
func (s *SwapService) ReconcilePendingSwap(ctx context.Context, transactionID string, now time.Time) (swapprovider.Outcome, error) {
	row, found, err := s.store.GetPendingSwapByID(ctx, transactionID)
	if err != nil {
		return swapprovider.OutcomeUnknown, err
	}
	if !found {
		return swapprovider.OutcomeUnknown, fmt.Errorf("pending swap %s not found", transactionID)
	}
	return s.reconcilePendingSwap(ctx, row, now)
}

func (s *SwapService) reconcilePendingSwap(ctx context.Context, row postgres.PendingSwapRow, now time.Time) (swapprovider.Outcome, error) {
	tx := row.Transaction
	logAttrs := []any{
		"transaction_id", tx.ID, "group_id", tx.GroupID, "action", tx.Action,
		"provider", row.Provider.String, "request_id", tx.ExecuteRequestID.String,
		"signed_tx_signature", row.SignedTxSignature.String, "age", now.Sub(tx.CreatedAt).Round(time.Second).String(),
	}

	provider, ok := s.providers[row.Provider.String]
	if !ok {
		err := fmt.Errorf("swap provider %q is not configured", row.Provider.String)
		logSwapBranchError("swap reconcile failed", err, append(logAttrs, "stage", "provider_lookup")...)
		return swapprovider.OutcomeUnknown, err
	}
	pending, err := s.pendingSwapFromRow(ctx, row)
	if err != nil {
		logSwapBranchError("swap reconcile failed", err, append(logAttrs, "stage", "load_swap")...)
		return swapprovider.OutcomeUnknown, err
	}

	resolution, err := provider.Resolve(ctx, pending)
	if err != nil {
		logSwapBranchError("swap reconcile failed", err, append(logAttrs, "stage", "resolve")...)
		return swapprovider.OutcomeUnknown, err
	}

	switch resolution.Outcome {
	case swapprovider.OutcomeFilled:
		confirmed, created, err := s.confirmSwap(ctx, tx.ID, pending.Request, resolution.Fill)
		if err != nil {
			logSwapBranchError("swap reconcile failed", err, append(logAttrs, "stage", "record_fill")...)
			return swapprovider.OutcomeUnknown, err
		}
		slog.Info("swap reconcile confirmed", append(logAttrs, "tx_signature", confirmed.TxSignature.String, "created", created)...)
		if created {
			recordReconciledSwap(confirmed, nil)
		}
		return swapprovider.OutcomeFilled, nil
	case swapprovider.OutcomeFailed:
		if err := s.failReconciledSwap(ctx, tx, resolution.Reason, now); err != nil {
			logSwapBranchError("swap reconcile failed", err, append(logAttrs, "stage", "mark_failed")...)
			return swapprovider.OutcomeUnknown, err
		}
		slog.Warn("swap reconcile failed swap", append(logAttrs, "reason", resolution.Reason)...)
		recordReconciledSwap(tx, errors.New(resolution.Reason))
		return swapprovider.OutcomeFailed, nil
	default:
		if now.Sub(tx.CreatedAt) >= swapUnresolvedAlertAge {
			slog.Error("swap unresolved", append(logAttrs, "reason", resolution.Reason)...)
			telemetry.Alert(ctx, telemetry.AlertEvent{
				Kind:     "swap_unresolved",
				Key:      "swap_unresolved:" + tx.ID,
				Severity: telemetry.SeverityCritical,
				Title:    "Treasury swap unresolved: outcome unknown, needs manual review",
				Detail:   "The swap was handed to the venue and neither the venue nor the chain can say whether it landed. It stays pending and its proposal is not retried.",
				Fields: map[string]string{
					"transaction_id": tx.ID,
					"group_id":       tx.GroupID,
					"action":         tx.Action,
					"provider":       row.Provider.String,
					"request_id":     tx.ExecuteRequestID.String,
					"tx_signature":   row.SignedTxSignature.String,
					"reason":         resolution.Reason,
					"age":            now.Sub(tx.CreatedAt).Round(time.Second).String(),
				},
			})
		} else {
			slog.Info("swap reconcile still pending", append(logAttrs, "reason", resolution.Reason)...)
		}
		return swapprovider.OutcomeUnknown, nil
	}
}

// recordReconciledSwap counts a swap the inline path reported as pending, now that it settled.
func recordReconciledSwap(tx postgres.TransactionRow, err error) {
	if tx.Action == postgres.TransactionActionSell {
		recordSwap(telemetry.EventSwapSell, tx.CostBasisAmount.Int64, true, err)
		return
	}
	recordSwap(telemetry.EventSwapBuy, tx.Amount, true, err)
}

// failReconciledSwap frees the proposal's execution slot and persists the retry backoff.
func (s *SwapService) failReconciledSwap(ctx context.Context, tx postgres.TransactionRow, reason string, now time.Time) error {
	if _, _, err := s.store.FailPendingSwap(ctx, tx.ID, reason); err != nil {
		return err
	}
	if !tx.ProposalID.Valid {
		return nil
	}
	attempts, err := s.store.GetProposalExecuteAttempts(ctx, tx.ProposalID.String)
	if err != nil {
		return err
	}
	return s.store.RecordProposalExecuteFailure(ctx, tx.ProposalID.String, now.Add(ProposalExecuteBackoff(attempts)), reason)
}

// pendingSwapFromRow rebuilds the swap the row was written for.
func (s *SwapService) pendingSwapFromRow(ctx context.Context, row postgres.PendingSwapRow) (swapprovider.PendingSwap, error) {
	tx := row.Transaction
	treasury, err := s.privy.EnsureTreasury(ctx, privy.GroupID(tx.GroupID))
	if err != nil {
		return swapprovider.PendingSwap{}, err
	}

	req := swapprovider.Request{
		GroupID:    tx.GroupID,
		InputMint:  tx.InputMint,
		OutputMint: tx.OutputMint,
		Amount:     tx.Amount,
		Wallet:     swapprovider.Wallet{PrivyWalletID: treasury.PrivyWalletID, SolanaAddress: treasury.SolanaAddress},
	}
	switch tx.Action {
	case postgres.TransactionActionBuy:
		req.Side = swapprovider.SideBuy
		req.Symbol = s.symbolForMint(ctx, tx.OutputMint)
		req.InputDecimals, req.OutputDecimals = usdcDecimals, jupiter.XStockDecimals
	case postgres.TransactionActionSell:
		req.Side = swapprovider.SideSell
		req.Symbol = s.symbolForMint(ctx, tx.InputMint)
		req.InputDecimals, req.OutputDecimals = jupiter.XStockDecimals, usdcDecimals
	default:
		return swapprovider.PendingSwap{}, fmt.Errorf("unsupported swap action %q", tx.Action)
	}

	pending := swapprovider.PendingSwap{
		Request:     req,
		RequestID:   tx.ExecuteRequestID.String,
		Submitted:   row.SubmittedAt.Valid,
		TxSignature: row.SignedTxSignature.String,
		Blockhash:   row.SignedTxBlockhash.String,
	}
	if row.SubmitExpiresAt.Valid {
		pending.ExpiresAt = row.SubmitExpiresAt.Time.UTC()
	}
	return pending, nil
}
