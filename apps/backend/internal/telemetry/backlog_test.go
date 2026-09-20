package telemetry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBacklogCollector_exportsQueueDepthAndOldestAge(t *testing.T) {
	// Arrange
	collector := newBacklogCollector(func(context.Context) (Backlog, error) {
		return Backlog{
			PendingDeposits:            2,
			OldestPendingDepositAgeSec: 930,
			PendingSwaps:               1,
			OldestPendingSwapAgeSec:    45,
			RedeemJobs:                 map[string]int64{"paying": 1},
			OldestRedeemJobAgeSec:      map[string]int64{"paying": 1200},
		}, nil
	})
	want := `
# HELP monaco_pending_deposit_oldest_age_seconds Age of the oldest pending deposit; 0 when none.
# TYPE monaco_pending_deposit_oldest_age_seconds gauge
monaco_pending_deposit_oldest_age_seconds 930
# HELP monaco_redeem_job_oldest_age_seconds Time since the last status change of the longest-waiting unsettled cash out job, by status; 0 when none.
# TYPE monaco_redeem_job_oldest_age_seconds gauge
monaco_redeem_job_oldest_age_seconds{status="debited"} 0
monaco_redeem_job_oldest_age_seconds{status="paying"} 1200
monaco_redeem_job_oldest_age_seconds{status="selling"} 0
# HELP monaco_backlog_up 1 when the backlog was read on the last scrape, else 0.
# TYPE monaco_backlog_up gauge
monaco_backlog_up 1
`

	// Act
	err := testutil.CollectAndCompare(collector, strings.NewReader(want),
		"monaco_pending_deposit_oldest_age_seconds", "monaco_redeem_job_oldest_age_seconds", "monaco_backlog_up")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
}

func TestBacklogCollector_sourceDown_dropsGaugesInsteadOfReportingZero(t *testing.T) {
	// Arrange
	collector := newBacklogCollector(func(context.Context) (Backlog, error) {
		return Backlog{}, errors.New("connection refused")
	})
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)

	// Act
	families, err := registry.Gather()

	// Assert
	if err != nil {
		t.Fatalf("Gather: %v (a dead database must not take the whole scrape down)", err)
	}
	if len(families) != 1 || families[0].GetName() != "monaco_backlog_up" || families[0].GetMetric()[0].GetGauge().GetValue() != 0 {
		t.Fatalf("families = %v, want only monaco_backlog_up 0: a fake empty queue hides the incident", families)
	}
}

func TestBacklogCollector_rapidScrapes_queryTheSourceOnce(t *testing.T) {
	// Arrange
	calls := 0
	collector := newBacklogCollector(func(context.Context) (Backlog, error) {
		calls++
		return Backlog{}, nil
	})

	// Act
	for range 5 {
		testutil.CollectAndCount(collector)
	}

	// Assert
	if calls != 1 {
		t.Fatalf("backlog queried %d times for 5 rapid scrapes, want 1", calls)
	}
}
