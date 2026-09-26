package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// PollerMatchups is the weekly matchup job's name in metrics, /health and alerts.
const PollerMatchups = "matchup_weeks"

// DefaultMatchupInterval is how often the matchup job checks whether a week has ended or needs
// drawing. A check with nothing to do is two indexed reads, so the week turns over within a few
// minutes of Monday 00:00 UTC without the job doing any real work the rest of the week.
const DefaultMatchupInterval = 5 * time.Minute

// matchupTickBudget bounds one pass. A draw values every eligible pot, which is the slow part.
const matchupTickBudget = 3 * time.Minute

// MatchupWeekRunner freezes ended weeks and draws the current one. *app.MatchupService implements it.
type MatchupWeekRunner interface {
	RunWeekly(ctx context.Context) error
}

// RunMatchupPoller runs the weekly job once at boot, so a week with no draw is drawn straight
// away, then on every interval until ctx ends.
func RunMatchupPoller(ctx context.Context, runner MatchupWeekRunner, interval time.Duration) {
	if runner == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultMatchupInterval
	}
	slog.Info("matchup poller started", "interval", interval)
	telemetry.RegisterPoller(PollerMatchups, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer slog.Info("matchup poller stopped")

	runTick := func() {
		telemetry.GuardTick(ctx, PollerMatchups, func() error {
			tickCtx, cancel := context.WithTimeout(ctx, matchupTickBudget)
			defer cancel()
			err := runner.RunWeekly(tickCtx)
			if err != nil && ctx.Err() == nil {
				slog.Error("matchup poller tick failed", "err", err)
			}
			return err
		})
	}

	runTick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		}
	}
}
