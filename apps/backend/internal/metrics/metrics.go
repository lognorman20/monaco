// Package metrics exposes the API's operational metrics in Prometheus format. The alerts
// built on them are listed in docs/how-to/observability.md.
package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "monaco"

// unmatchedRoute labels requests no route pattern matched (404s, scanners), so arbitrary
// paths cannot grow the label set without bound.
const unmatchedRoute = "unmatched"

// Metrics owns the registry and the instruments the API writes to. It implements
// worker.TickObserver and worker.RestartObserver.
type Metrics struct {
	registry *prometheus.Registry

	workerTicks        *prometheus.CounterVec
	workerTickDuration *prometheus.HistogramVec
	workerLastSuccess  *prometheus.GaugeVec
	workerRestarts     *prometheus.CounterVec

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
}

// New returns Metrics with the Go runtime and process collectors registered.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		workerTicks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_ticks_total",
			Help: "Background worker ticks by result (success or failure).",
		}, []string{"worker", "result"}),
		workerTickDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "worker_tick_duration_seconds",
			Help:    "Duration of one background worker tick.",
			Buckets: []float64{0.05, 0.25, 1, 2.5, 5, 10, 20, 30, 60, 120},
		}, []string{"worker"}),
		workerLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Name: "worker_last_success_timestamp_seconds",
			Help: "Unix time of the last successful tick of a background worker.",
		}, []string{"worker"}),
		workerRestarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_restarts_total",
			Help: "Times the supervisor restarted a background worker after a panic or an unexpected exit.",
		}, []string{"worker"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "http_requests_total",
			Help: "HTTP requests by route pattern, method and status code.",
		}, []string{"route", "method", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds",
			Help: "HTTP request latency by route pattern and method.",
			// Trade and cash-out routes confirm on Solana inside the request.
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 180},
		}, []string{"route", "method"}),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.workerTicks, m.workerTickDuration, m.workerLastSuccess, m.workerRestarts,
		m.httpRequests, m.httpDuration,
	)
	return m
}

// RegisterWorkers zeroes the series of each worker so "no restarts" and "no failures" are
// a visible 0 instead of an absent series an alert rule cannot evaluate.
func (m *Metrics) RegisterWorkers(workers ...string) {
	for _, worker := range workers {
		m.workerTicks.WithLabelValues(worker, resultSuccess)
		m.workerTicks.WithLabelValues(worker, resultFailure)
		m.workerRestarts.WithLabelValues(worker)
	}
}

// RegisterDB exports database/sql pool statistics (open, in use, wait count).
func (m *Metrics) RegisterDB(db *sql.DB) {
	m.registry.MustRegister(collectors.NewDBStatsCollector(db, namespace))
}

// RegisterOps exports the money-path backlog gauges read from source on every scrape.
func (m *Metrics) RegisterOps(source OpsSource) {
	m.registry.MustRegister(newOpsCollector(source))
}

const (
	resultSuccess = "success"
	resultFailure = "failure"
)

// ObserveTick implements worker.TickObserver.
func (m *Metrics) ObserveTick(worker string, duration time.Duration, err error) {
	m.workerTickDuration.WithLabelValues(worker).Observe(duration.Seconds())
	if err != nil {
		m.workerTicks.WithLabelValues(worker, resultFailure).Inc()
		return
	}
	m.workerTicks.WithLabelValues(worker, resultSuccess).Inc()
	m.workerLastSuccess.WithLabelValues(worker).SetToCurrentTime()
}

// WorkerRestarted implements worker.RestartObserver.
func (m *Metrics) WorkerRestarted(worker string) {
	m.workerRestarts.WithLabelValues(worker).Inc()
}

// Handler serves the registry in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// HTTPMiddleware counts every request and times it. Install it outside panic recovery so
// the 500 written for a panic is counted, and inside nothing that copies the request: the
// route label is the pattern http.ServeMux stores on the request it was handed.
func (m *Metrics) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				route := r.Pattern
				if route == "" {
					route = unmatchedRoute
				}
				m.httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(recorder.status)).Inc()
				m.httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.wrote = true
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush forwards to the underlying writer when it supports flushing.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
