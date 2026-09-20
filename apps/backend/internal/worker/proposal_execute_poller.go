package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// DefaultProposalExecuteInterval is how often passed proposals are executed.
const DefaultProposalExecuteInterval = 15 * time.Second

// ProposalExecuteLease is how long a claimed proposal is withheld from every other executor.
// It outlasts the longest inline execute (onchain setup plus fill polling); a crashed executor's
// proposal comes back after it, and any swap it submitted still holds the execution slot.
const ProposalExecuteLease = 5 * time.Minute

// ProposalExecutePoller runs swaps for passed proposals that have no pending or confirmed swap.
// Claims and retry backoff live in Postgres, so any number of API instances share them.
type ProposalExecutePoller struct {
	store *postgres.Store
	exec  *app.ExecuteOnPassService
	clock Clock
	limit int
}

// NewProposalExecutePoller wires execute-on-pass polling dependencies.
func NewProposalExecutePoller(store *postgres.Store, exec *app.ExecuteOnPassService, clock Clock) *ProposalExecutePoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &ProposalExecutePoller{
		store: store,
		exec:  exec,
		clock: clock,
		limit: 20,
	}
}

// RunProposalExecutePoller ticks until ctx is cancelled.
func RunProposalExecutePoller(ctx context.Context, poller *ProposalExecutePoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultProposalExecuteInterval
	}

	logProposalExecutePollerStarted(interval)
	telemetry.RegisterPoller(PollerProposalExecute, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer logProposalExecutePollerStopped()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			telemetry.GuardTick(ctx, PollerProposalExecute, func() error {
				poller.tick(ctx)
				return nil
			})
		}
	}
}

func (p *ProposalExecutePoller) tick(ctx context.Context) {
	if p == nil || p.store == nil || p.exec == nil {
		return
	}

	now := p.clock.Now()
	rows, err := p.store.ClaimPassedProposalsForExecute(ctx, now, ProposalExecuteLease, p.limit)
	if err != nil {
		logProposalExecutePollerListFailed(err)
		return
	}

	logProposalExecutePollerTickStart(len(rows))
	var tickErr error
	for _, claimed := range rows {
		row := claimed.Proposal
		proposal := app.Proposal{
			ID:          row.ID,
			GroupID:     row.GroupID,
			ProposerID:  row.ProposerID,
			Symbol:      row.Symbol,
			Kind:        row.Kind,
			UsdcMicros:  row.UsdcMicros,
			TokenAmount: row.TokenAmount,
			Status:      row.Status,
			ExpiresAt:   row.ExpiresAt.UTC().Unix(),
		}
		result, err := p.exec.ExecuteOnPass(ctx, proposal)
		if errors.Is(err, app.ErrSwapOutcomeUnknown) || errors.Is(err, app.ErrSwapInFlight) {
			// The pending swap holds the proposal's slot; the swap reconciler decides what happens next.
			logProposalExecuteLeftPending(proposal.ID, proposal.GroupID, proposal.Symbol, err)
			continue
		}
		if err != nil {
			tickErr = err
			p.recordExecuteFailure(ctx, proposal.ID, claimed.ExecuteAttempts, err)
			logProposalExecuteFailed(proposal.ID, proposal.GroupID, proposal.Symbol, proposal.UsdcMicros, "execute_on_pass", err)
			continue
		}
		txID := result.Transaction.ID
		sig := ""
		if result.Transaction.TxSignature.Valid {
			sig = result.Transaction.TxSignature.String
		}
		logProposalExecuteSuccess(proposal.ID, txID, sig, result.Created)
	}
	logProposalExecutePollerTickEnd(len(rows), tickErr)
}

func (p *ProposalExecutePoller) recordExecuteFailure(ctx context.Context, proposalID string, attempts int, cause error) {
	next := p.clock.Now().Add(app.ProposalExecuteBackoff(attempts))
	if err := p.store.RecordProposalExecuteFailure(ctx, proposalID, next, cause.Error()); err != nil {
		// The claim lease still withholds the proposal, so a lost write only lengthens the wait.
		slog.Error("proposal execute backoff record failed", "proposal_id", proposalID, "err", err)
	}
}

func logProposalExecutePollerStarted(interval time.Duration) {
	slog.Info("proposal execute poller started", "interval", interval)
}

func logProposalExecutePollerStopped() {
	slog.Info("proposal execute poller stopped")
}

func logProposalExecutePollerTickStart(count int) {
	slog.Info("proposal execute poller tick start", "pending_count", count)
}

func logProposalExecutePollerTickEnd(count int, err error) {
	args := []any{"pending_count", count}
	if err != nil {
		args = append(args, "err", err)
		slog.Error("proposal execute poller tick end", args...)
		return
	}
	slog.Info("proposal execute poller tick end", args...)
}

func logProposalExecutePollerListFailed(err error) {
	slog.Error("proposal execute poller list pending failed", "err", err)
}

func logProposalExecuteFailed(proposalID, groupID, symbol string, usdcMicros int64, stage string, err error) {
	slog.Error("proposal execute failed",
		"proposal_id", proposalID,
		"group_id", groupID,
		"symbol", symbol,
		"usdc_amount", usdcMicros,
		"stage", stage,
		"err", err,
	)
}

func logProposalExecuteLeftPending(proposalID, groupID, symbol string, err error) {
	slog.Warn("proposal execute left pending for reconcile",
		"proposal_id", proposalID,
		"group_id", groupID,
		"symbol", symbol,
		"err", err,
	)
}

func logProposalExecuteSuccess(proposalID, transactionID, txSignature string, created bool) {
	slog.Info("proposal execute success",
		"proposal_id", proposalID,
		"transaction_id", transactionID,
		"tx_signature", txSignature,
		"created", created,
	)
}
