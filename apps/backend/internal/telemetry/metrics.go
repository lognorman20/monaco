// Package telemetry is the API's metrics, alerting, error reporting and poller liveness.
//
// It is a leaf package: it imports nothing from the rest of the backend, so any layer may
// record into it. Everything here is safe to call before Init* and from any goroutine, and
// nothing here may block or fail a money path.
package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Outcome labels shared by the counters below. Keep the set small: every distinct value
// is a time series.
const (
	OutcomeOK          = "ok"
	OutcomeError       = "error"
	OutcomeRejected    = "rejected"
	OutcomeRateLimited = "rate_limited"
	OutcomePanic       = "panic"
)

// Money event names. These are the label values dashboards and alerts key on, so they are
// constants rather than strings at the call site.
const (
	EventDepositSweep = "deposit_sweep"
	EventSwapBuy      = "swap_buy"
	EventSwapSell     = "swap_sell"
	EventRedeem       = "redeem"
	EventRedeemRescue = "redeem_recovery"
	EventWithdrawal   = "withdrawal"
	EventAgentIntent  = "agent_intent"
)

var registry = newRegistry()

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_http_requests_total",
		Help: "HTTP requests served, by route pattern, method and status code.",
	}, []string{"route", "method", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "monaco_http_request_duration_seconds",
		Help: "HTTP request latency by route pattern and method.",
		// Trade and cash out routes confirm a Solana transaction inside the request, so the
		// buckets run well past the usual web range.
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	}, []string{"route", "method"})

	moneyEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_money_events_total",
		Help: "Money-moving operations by event and outcome.",
	}, []string{"event", "outcome"})

	moneyVolume = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_money_volume_usdc_micros_total",
		Help: "USDC micros moved by successful money events.",
	}, []string{"event"})

	upstreamRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_upstream_requests_total",
		Help: "Outbound HTTP calls by upstream service and outcome.",
	}, []string{"service", "outcome"})

	upstreamDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "monaco_upstream_request_duration_seconds",
		Help:    "Outbound HTTP latency by upstream service.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"service"})

	pollerTicks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_poller_ticks_total",
		Help: "Background poller ticks by poller and outcome.",
	}, []string{"poller", "outcome"})

	pollerTickDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "monaco_poller_tick_duration_seconds",
		Help:    "Background poller tick duration.",
		Buckets: []float64{0.01, 0.1, 0.5, 1, 5, 15, 30, 60},
	}, []string{"poller"})

	pollerLastTick = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "monaco_poller_last_tick_timestamp_seconds",
		Help: "Unix time the poller last finished a tick. Alert when this stops advancing.",
	}, []string{"poller"})

	relayerBalance = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "monaco_relayer_balance_lamports",
		Help: "SOL balance of the fee-paying relayer, in lamports. Every transaction fails at zero.",
	})

	breakerOpens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_price_breaker_opens_total",
		Help: "Times a price source circuit breaker opened.",
	}, []string{"source"})

	priceFallbacks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_price_fallbacks_total",
		Help: "Holdings valued by a fallback tier instead of the primary price source.",
	}, []string{"tier"})

	alertsSent = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "monaco_alerts_total",
		Help: "Alerts raised by kind and delivery result.",
	}, []string{"kind", "delivery"})
)

func newRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return r
}

func init() {
	registry.MustRegister(
		httpRequests, httpDuration,
		moneyEvents, moneyVolume,
		upstreamRequests, upstreamDuration,
		pollerTicks, pollerTickDuration, pollerLastTick,
		relayerBalance, breakerOpens, priceFallbacks, alertsSent,
	)
}

// Handler serves the Prometheus exposition of every metric in this package.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})
}

// ObserveHTTP records one served request. route must be the mux pattern, never the raw
// path: ids in a label would mint a time series per cabal.
func ObserveHTTP(route, method string, status int, elapsed time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	httpRequests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(route, method).Observe(elapsed.Seconds())
}

// MoneyEvent counts one money-moving operation.
func MoneyEvent(event, outcome string) {
	moneyEvents.WithLabelValues(event, outcome).Inc()
}

// MoneyMoved counts a successful money event and the USDC it moved.
func MoneyMoved(event string, usdcMicros int64) {
	moneyEvents.WithLabelValues(event, OutcomeOK).Inc()
	if usdcMicros > 0 {
		moneyVolume.WithLabelValues(event).Add(float64(usdcMicros))
	}
}

// ObserveUpstream records one outbound call.
func ObserveUpstream(service, outcome string, elapsed time.Duration) {
	upstreamRequests.WithLabelValues(service, outcome).Inc()
	upstreamDuration.WithLabelValues(service).Observe(elapsed.Seconds())
}

// SetRelayerBalance publishes the relayer's SOL balance.
func SetRelayerBalance(lamports uint64) {
	relayerBalance.Set(float64(lamports))
}

// BreakerOpened counts a price source breaker opening.
func BreakerOpened(source string) {
	breakerOpens.WithLabelValues(source).Inc()
}

// PriceFallback counts one holding valued by a fallback tier ("jupiter", "cost_basis").
func PriceFallback(tier string) {
	priceFallbacks.WithLabelValues(tier).Inc()
}
