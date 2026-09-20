package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// opsScrapeTimeout bounds the backlog queries of one scrape.
	opsScrapeTimeout = 5 * time.Second
	// opsCacheTTL keeps a fast or duplicated scraper from turning into database load.
	opsCacheTTL = 10 * time.Second
)

// OpsBacklog is the state of the money paths at one instant. Ages are whole seconds and 0
// when the queue is empty.
type OpsBacklog struct {
	PendingDeposits            int64
	OldestPendingDepositAgeSec int64
	PendingSwaps               int64
	OldestPendingSwapAgeSec    int64
	// RedeemJobs and OldestRedeemJobAgeSec are keyed by unsettled job status (debited,
	// selling, paying); age counts from the job's last status change.
	RedeemJobs            map[string]int64
	OldestRedeemJobAgeSec map[string]int64
}

// OpsSource reads what the ops gauges report. Either func may be nil.
type OpsSource struct {
	Backlog func(ctx context.Context) (OpsBacklog, error)
	// FeePayerLamports is the relayer's SOL balance; at zero no sweep, swap or payout lands.
	FeePayerLamports func(ctx context.Context) (uint64, error)
}

// redeemJobStatuses are always exported, so an empty queue reads 0 rather than absent.
var redeemJobStatuses = []string{"debited", "selling", "paying"}

type opsCollector struct {
	source OpsSource

	pendingDeposits  *prometheus.Desc
	oldestDepositAge *prometheus.Desc
	pendingSwaps     *prometheus.Desc
	oldestSwapAge    *prometheus.Desc
	redeemJobs       *prometheus.Desc
	oldestRedeemAge  *prometheus.Desc
	feePayerLamports *prometheus.Desc
	sourceUp         *prometheus.Desc
	mu               sync.Mutex
	cachedAt         time.Time
	cached           []prometheus.Metric
}

func newOpsCollector(source OpsSource) *opsCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
	}
	return &opsCollector{
		source:           source,
		pendingDeposits:  desc("pending_deposits", "Deposits waiting for their sweep to confirm."),
		oldestDepositAge: desc("pending_deposit_oldest_age_seconds", "Age of the oldest pending deposit; 0 when none."),
		pendingSwaps:     desc("pending_swaps", "Treasury swaps recorded as pending."),
		oldestSwapAge:    desc("pending_swap_oldest_age_seconds", "Age of the oldest pending swap; 0 when none."),
		redeemJobs:       desc("redeem_jobs", "Unsettled cash-out jobs by status.", "status"),
		oldestRedeemAge:  desc("redeem_job_oldest_age_seconds", "Time since the last status change of the longest-waiting unsettled cash-out job, by status; 0 when none.", "status"),
		feePayerLamports: desc("fee_payer_lamports", "SOL balance of the relayer fee payer, in lamports."),
		sourceUp:         desc("ops_source_up", "1 when the source of the ops gauges answered on the last scrape, else 0.", "source"),
	}
}

func (c *opsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.pendingDeposits, c.oldestDepositAge, c.pendingSwaps, c.oldestSwapAge,
		c.redeemJobs, c.oldestRedeemAge, c.feePayerLamports, c.sourceUp,
	} {
		ch <- d
	}
}

func (c *opsCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedAt.IsZero() || time.Since(c.cachedAt) >= opsCacheTTL {
		c.cached = c.read()
		c.cachedAt = time.Now()
	}
	for _, metric := range c.cached {
		ch <- metric
	}
}

// read queries the sources. A failing source drops its gauges for this scrape and sets
// ops_source_up to 0: a stale or zero backlog would hide exactly the incident it is for.
func (c *opsCollector) read() []prometheus.Metric {
	ctx, cancel := context.WithTimeout(context.Background(), opsScrapeTimeout)
	defer cancel()

	gauge := func(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
		return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	}
	up := func(source string, err error) prometheus.Metric {
		if err != nil {
			slog.Warn("metrics ops source failed", "source", source, "err", err)
			return gauge(c.sourceUp, 0, source)
		}
		return gauge(c.sourceUp, 1, source)
	}

	var out []prometheus.Metric
	if c.source.Backlog != nil {
		backlog, err := c.source.Backlog(ctx)
		out = append(out, up("database", err))
		if err == nil {
			out = append(out,
				gauge(c.pendingDeposits, float64(backlog.PendingDeposits)),
				gauge(c.oldestDepositAge, float64(backlog.OldestPendingDepositAgeSec)),
				gauge(c.pendingSwaps, float64(backlog.PendingSwaps)),
				gauge(c.oldestSwapAge, float64(backlog.OldestPendingSwapAgeSec)),
			)
			for _, status := range redeemJobStatuses {
				out = append(out,
					gauge(c.redeemJobs, float64(backlog.RedeemJobs[status]), status),
					gauge(c.oldestRedeemAge, float64(backlog.OldestRedeemJobAgeSec[status]), status),
				)
			}
		}
	}
	if c.source.FeePayerLamports != nil {
		lamports, err := c.source.FeePayerLamports(ctx)
		out = append(out, up("solana_rpc", err))
		if err == nil {
			out = append(out, gauge(c.feePayerLamports, float64(lamports)))
		}
	}
	return out
}
