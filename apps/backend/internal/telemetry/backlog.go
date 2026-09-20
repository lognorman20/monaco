package telemetry

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

const (
	// backlogScrapeTimeout bounds the backlog queries of one scrape.
	backlogScrapeTimeout = 5 * time.Second
	// backlogCacheTTL keeps a fast or duplicated scraper from turning into database load.
	backlogCacheTTL = 10 * time.Second
)

// Backlog is the money still in flight at one instant. The event counters say what
// happened; this says what is stuck: a deposit whose sweep never confirmed or a cash out
// sitting in paying raises no error and no panic, it just gets older. Ages are whole
// seconds and 0 when the queue is empty.
type Backlog struct {
	PendingDeposits            int64
	OldestPendingDepositAgeSec int64
	PendingSwaps               int64
	OldestPendingSwapAgeSec    int64
	// RedeemJobs and OldestRedeemJobAgeSec are keyed by unsettled job status (debited,
	// selling, paying); age counts from the job's last status change.
	RedeemJobs            map[string]int64
	OldestRedeemJobAgeSec map[string]int64
}

// BacklogSource reads the backlog. *postgres.Store provides it through a small adapter in
// cmd/api, so this package stays a leaf.
type BacklogSource func(ctx context.Context) (Backlog, error)

// redeemJobStatuses are always exported, so an empty queue reads 0 rather than absent.
var redeemJobStatuses = []string{"debited", "selling", "paying"}

// RegisterBacklog exports the backlog gauges, read from source on scrape. Call it once at boot.
func RegisterBacklog(source BacklogSource) error {
	return registry.Register(newBacklogCollector(source))
}

// RegisterDBStats exports database/sql pool statistics (open, in use, wait count) so a
// saturated pool shows up before requests start timing out. Call it once at boot.
func RegisterDBStats(db *sql.DB, name string) error {
	return registry.Register(collectors.NewDBStatsCollector(db, name))
}

type backlogCollector struct {
	source BacklogSource

	pendingDeposits  *prometheus.Desc
	oldestDepositAge *prometheus.Desc
	pendingSwaps     *prometheus.Desc
	oldestSwapAge    *prometheus.Desc
	redeemJobs       *prometheus.Desc
	oldestRedeemAge  *prometheus.Desc
	sourceUp         *prometheus.Desc

	mu       sync.Mutex
	cachedAt time.Time
	cached   []prometheus.Metric
}

func newBacklogCollector(source BacklogSource) *backlogCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(name, help, labels, nil)
	}
	return &backlogCollector{
		source:           source,
		pendingDeposits:  desc("monaco_pending_deposits", "Deposits waiting for their sweep to confirm."),
		oldestDepositAge: desc("monaco_pending_deposit_oldest_age_seconds", "Age of the oldest pending deposit; 0 when none."),
		pendingSwaps:     desc("monaco_pending_swaps", "Treasury swaps recorded as pending."),
		oldestSwapAge:    desc("monaco_pending_swap_oldest_age_seconds", "Age of the oldest pending swap; 0 when none."),
		redeemJobs:       desc("monaco_redeem_jobs", "Unsettled cash out jobs by status.", "status"),
		oldestRedeemAge:  desc("monaco_redeem_job_oldest_age_seconds", "Time since the last status change of the longest-waiting unsettled cash out job, by status; 0 when none.", "status"),
		sourceUp:         desc("monaco_backlog_up", "1 when the backlog was read on the last scrape, else 0."),
	}
}

func (c *backlogCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.pendingDeposits, c.oldestDepositAge, c.pendingSwaps, c.oldestSwapAge,
		c.redeemJobs, c.oldestRedeemAge, c.sourceUp,
	} {
		ch <- d
	}
}

func (c *backlogCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedAt.IsZero() || time.Since(c.cachedAt) >= backlogCacheTTL {
		c.cached = c.read()
		c.cachedAt = time.Now()
	}
	for _, metric := range c.cached {
		ch <- metric
	}
}

// read queries the source. On failure the gauges are dropped for this scrape and
// monaco_backlog_up is 0: a stale or zero backlog would hide exactly the incident it is for.
func (c *backlogCollector) read() []prometheus.Metric {
	ctx, cancel := context.WithTimeout(context.Background(), backlogScrapeTimeout)
	defer cancel()

	gauge := func(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
		return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	}

	backlog, err := c.source(ctx)
	if err != nil {
		slog.Warn("metrics backlog read failed", "err", err)
		return []prometheus.Metric{gauge(c.sourceUp, 0)}
	}
	out := []prometheus.Metric{
		gauge(c.sourceUp, 1),
		gauge(c.pendingDeposits, float64(backlog.PendingDeposits)),
		gauge(c.oldestDepositAge, float64(backlog.OldestPendingDepositAgeSec)),
		gauge(c.pendingSwaps, float64(backlog.PendingSwaps)),
		gauge(c.oldestSwapAge, float64(backlog.OldestPendingSwapAgeSec)),
	}
	for _, status := range redeemJobStatuses {
		out = append(out,
			gauge(c.redeemJobs, float64(backlog.RedeemJobs[status]), status),
			gauge(c.oldestRedeemAge, float64(backlog.OldestRedeemJobAgeSec[status]), status),
		)
	}
	return out
}
