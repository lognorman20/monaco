package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// DefaultProposalExecuteInterval is how often passed proposals are executed.
const DefaultProposalExecuteInterval = 15 * time.Second

const maxProposalExecuteBackoff = 15 * time.Minute

// ProposalExecutePoller runs Jupiter buys for passed proposals without confirmed swaps.
type ProposalExecutePoller struct {
	store    *postgres.Store
	exec     *app.ExecuteOnPassService
	clock    Clock
	limit    int
	backoff  map[string]time.Time
	failures map[string]int
}

// NewProposalExecutePoller wires execute-on-pass polling dependencies.
func NewProposalExecutePoller(store *postgres.Store, exec *app.ExecuteOnPassService, clock Clock) *ProposalExecutePoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &ProposalExecutePoller{
		store:    store,
		exec:     exec,
		clock:    clock,
		limit:    20,
		backoff:  make(map[string]time.Time),
		failures: make(map[string]int),
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

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer logProposalExecutePollerStopped()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poller.tick(ctx)
		}
	}
}

func (p *ProposalExecutePoller) tick(ctx context.Context) {
	if p == nil || p.store == nil || p.exec == nil {
		return
	}

	rows, err := p.store.ListPassedProposalsPendingExecute(ctx, p.limit)
	if err != nil {
		logProposalExecutePollerListFailed(err)
		return
	}

	logProposalExecutePollerTickStart(len(rows))
	var tickErr error
	now := p.clock.Now()
	for _, row := range rows {
		if p.shouldSkipExecute(row.ID, now) {
			continue
		}
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
		if err != nil {
			tickErr = err
			p.recordExecuteFailure(proposal.ID, now)
			logProposalExecuteFailed(proposal.ID, proposal.GroupID, proposal.Symbol, proposal.UsdcMicros, "execute_on_pass", err)
			continue
		}
		p.clearExecuteFailure(proposal.ID)
		txID := result.Transaction.ID
		sig := ""
		if result.Transaction.TxSignature.Valid {
			sig = result.Transaction.TxSignature.String
		}
		logProposalExecuteSuccess(proposal.ID, txID, sig, result.Created)
	}
	logProposalExecutePollerTickEnd(len(rows), tickErr)
}

func (p *ProposalExecutePoller) shouldSkipExecute(proposalID string, now time.Time) bool {
	if p == nil {
		return false
	}
	next, ok := p.backoff[proposalID]
	return ok && now.Before(next)
}

func (p *ProposalExecutePoller) recordExecuteFailure(proposalID string, now time.Time) {
	if p == nil {
		return
	}
	count := p.failures[proposalID] + 1
	p.failures[proposalID] = count
	delay := DefaultProposalExecuteInterval
	for i := 1; i < count && delay < maxProposalExecuteBackoff; i++ {
		delay *= 2
	}
	p.backoff[proposalID] = now.Add(delay)
}

func (p *ProposalExecutePoller) clearExecuteFailure(proposalID string) {
	if p == nil {
		return
	}
	delete(p.backoff, proposalID)
	delete(p.failures, proposalID)
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

func logProposalExecuteSuccess(proposalID, transactionID, txSignature string, created bool) {
	slog.Info("proposal execute success",
		"proposal_id", proposalID,
		"transaction_id", transactionID,
		"tx_signature", txSignature,
		"created", created,
	)
}
