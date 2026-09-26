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

// PollerNotifications is the notification poller's name in metrics, /health and alerts.
const PollerNotifications = "notifications"

// DefaultNotificationInterval is how often the poller looks for votes about to close.
const DefaultNotificationInterval = time.Minute

// Balance watch pacing: each member with a push device registered in the last
// balanceWatchActiveFor has their balance read at most every balanceWatchEvery, at most
// balanceWatchBatch members per tick, oldest reading first. One chain read per member.
const (
	balanceWatchEvery     = 2 * time.Minute
	balanceWatchBatch     = 25
	balanceWatchActiveFor = 60 * 24 * time.Hour
)

// ProposalReminder is the governance work the poller drives. *app.GovernanceService implements it.
type ProposalReminder interface {
	FinalizeExpiredProposal(ctx context.Context, proposalID string) (app.Proposal, error)
	RemindClosingProposal(ctx context.Context, row postgres.ProposalRow) (int, error)
}

// MemberBalanceReader reads a member wallet's USDC. privy.Client implements it.
type MemberBalanceReader interface {
	MemberUSDCBalance(ctx context.Context, memberAddress string) (int64, error)
}

// NotificationPoller does the notification work nothing else triggers:
//   - closes votes that ran out of time, so the cabal hears "ran out of time" even when no one
//     opens the proposal (expiry used to be noticed only on read);
//   - sends "closes in 1 hour" to voters who have not voted, once per proposal;
//   - reads the balance of members who turned push on, so money arriving from outside reaches
//     them as "arrived in your balance" without opening the app.
type NotificationPoller struct {
	store      *postgres.Store
	governance ProposalReminder
	notifier   *app.Notifier
	balances   MemberBalanceReader
	clock      Clock
	limit      int
}

// NewNotificationPoller wires the poller. balances may be nil to skip the balance watch.
func NewNotificationPoller(store *postgres.Store, governance ProposalReminder, notifier *app.Notifier, balances MemberBalanceReader, clock Clock) *NotificationPoller {
	if clock == nil {
		clock = systemClock{}
	}
	return &NotificationPoller{store: store, governance: governance, notifier: notifier, balances: balances, clock: clock, limit: 50}
}

// RunNotificationPoller ticks until ctx is cancelled.
func RunNotificationPoller(ctx context.Context, poller *NotificationPoller, interval time.Duration) {
	if poller == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultNotificationInterval
	}
	slog.Info("notification poller started", "interval", interval)
	telemetry.RegisterPoller(PollerNotifications, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer slog.Info("notification poller stopped")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			telemetry.GuardTick(ctx, PollerNotifications, func() error {
				return poller.Tick(ctx)
			})
		}
	}
}

// Tick runs each job once. A job that fails does not stop the others; the first error is returned.
func (p *NotificationPoller) Tick(ctx context.Context) error {
	if p == nil || p.store == nil || p.governance == nil {
		return nil
	}
	now := p.clock.Now().UTC()
	errs := []error{
		p.finalizeExpired(ctx, now),
		p.remindClosing(ctx, now),
		p.watchBalances(ctx, now),
	}
	return errors.Join(errs...)
}

func (p *NotificationPoller) finalizeExpired(ctx context.Context, now time.Time) error {
	ids, err := p.store.ListExpiredOpenProposalIDs(ctx, now, p.limit)
	if err != nil {
		slog.ErrorContext(ctx, "notification poller list expired failed", "err", err)
		return err
	}
	var firstErr error
	for _, id := range ids {
		if _, err := p.governance.FinalizeExpiredProposal(ctx, id); err != nil {
			if errors.Is(err, app.ErrProposalNotFound) {
				// Closed or removed between the list and now, by a request or another
				// instance. There is nothing left to do for it, and the tick goes on.
				continue
			}
			slog.ErrorContext(ctx, "notification poller finalize expired failed", "proposal_id", id, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if len(ids) > 0 {
		slog.InfoContext(ctx, "notification poller closed expired votes", "count", len(ids))
	}
	return firstErr
}

func (p *NotificationPoller) remindClosing(ctx context.Context, now time.Time) error {
	rows, err := p.store.ListProposalsClosingSoon(ctx, now, now.Add(app.ClosingReminderLead), app.ClosingReminderMinWindow, p.limit)
	if err != nil {
		slog.ErrorContext(ctx, "notification poller list closing failed", "err", err)
		return err
	}
	var firstErr error
	for _, row := range rows {
		reminded, err := p.governance.RemindClosingProposal(ctx, row)
		if err != nil {
			if errors.Is(err, app.ErrProposalNotFound) {
				// The vote ended between the list and the reminder; nobody is waiting on it.
				continue
			}
			slog.ErrorContext(ctx, "notification poller closing reminder failed", "proposal_id", row.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		slog.InfoContext(ctx, "notification poller closing reminder sent", "proposal_id", row.ID, "reminded", reminded)
	}
	return firstErr
}

func (p *NotificationPoller) watchBalances(ctx context.Context, now time.Time) error {
	if p.balances == nil || p.notifier == nil {
		return nil
	}
	candidates, err := p.store.ListBalanceWatchCandidates(ctx, now.Add(-balanceWatchActiveFor), now.Add(-balanceWatchEvery), balanceWatchBatch)
	if err != nil {
		slog.ErrorContext(ctx, "notification poller list balance watch failed", "err", err)
		return err
	}
	for _, c := range candidates {
		balance, err := p.balances.MemberUSDCBalance(ctx, c.WalletAddress)
		if err != nil {
			// A chain read that failed says nothing about the balance; try this member next time.
			slog.WarnContext(ctx, "notification poller balance read failed", "user_id", c.UserID, "err", err)
			continue
		}
		p.notifier.ObserveMemberBalance(ctx, c.UserID, c.WalletAddress, balance)
	}
	return nil
}
