package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/app"
	"github.com/monaco/monaco/apps/backend/internal/postgres"
)

// DefaultAgentDeploymentInterval is how often agent deployments are advanced.
const DefaultAgentDeploymentInterval = 20 * time.Second

const maxAgentDeploymentBackoff = 15 * time.Minute

// AgentDeploymentPoller sends SPL USDC for passed deploy_agent votes and watches the Solana
// treasury for USDC coming back from recalled agent wallets. It is separate from the sweep
// poller: it never credits share units and never touches member deposits.
type AgentDeploymentPoller struct {
	store    *postgres.Store
	svc      *app.AgentDeploymentService
	clock    Clock
	limit    int
	backoff  map[string]time.Time
	failures map[string]int
}

// NewAgentDeploymentPoller wires the agent deployment job.
func NewAgentDeploymentPoller(store *postgres.Store, svc *app.AgentDeploymentService, clock Clock) *AgentDeploymentPoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &AgentDeploymentPoller{
		store:    store,
		svc:      svc,
		clock:    clock,
		limit:    20,
		backoff:  make(map[string]time.Time),
		failures: make(map[string]int),
	}
}

// RunAgentDeploymentPoller ticks until ctx is cancelled.
func RunAgentDeploymentPoller(ctx context.Context, poller *AgentDeploymentPoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultAgentDeploymentInterval
	}
	slog.Info("agent deployment poller started", "interval", interval)
	defer slog.Info("agent deployment poller stopped")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poller.Tick(ctx)
		}
	}
}

// Tick advances every pending_transfer deployment (step 1, send) and every recalling
// deployment (step 3, inbound confirm). Step 2 is the agent's own trading; Monaco does nothing.
func (p *AgentDeploymentPoller) Tick(ctx context.Context) {
	if p == nil || p.store == nil || p.svc == nil {
		return
	}
	p.run(ctx, postgres.AgentDeploymentPendingTransfer, p.svc.ProcessPendingTransfer)
	p.run(ctx, postgres.AgentDeploymentRecalling, p.svc.ProcessRecalling)
}

func (p *AgentDeploymentPoller) run(
	ctx context.Context,
	status postgres.AgentDeploymentStatus,
	step func(context.Context, postgres.AgentDeploymentRow) (app.AgentDeploymentOutcome, error),
) {
	rows, err := p.store.ListAgentDeploymentsByStatus(ctx, status, p.limit)
	if err != nil {
		slog.Error("agent deployment poller list failed", "status", status, "err", err)
		return
	}
	now := p.clock.Now()
	for _, row := range rows {
		if next, ok := p.backoff[row.ID]; ok && now.Before(next) {
			continue
		}
		outcome, err := step(ctx, row)
		if err != nil {
			p.recordFailure(row.ID, now)
			slog.Error("agent deployment step failed",
				"deployment_id", row.ID,
				"group_id", row.GroupID,
				"status", status,
				"err", err,
			)
			continue
		}
		delete(p.backoff, row.ID)
		delete(p.failures, row.ID)
		if outcome != app.AgentDeploymentOutcomeWaiting {
			slog.Info("agent deployment step", "deployment_id", row.ID, "status", status, "outcome", outcome)
		}
	}
}

func (p *AgentDeploymentPoller) recordFailure(id string, now time.Time) {
	count := p.failures[id] + 1
	p.failures[id] = count
	delay := DefaultAgentDeploymentInterval
	for i := 1; i < count && delay < maxAgentDeploymentBackoff; i++ {
		delay *= 2
	}
	p.backoff[id] = now.Add(delay)
}
