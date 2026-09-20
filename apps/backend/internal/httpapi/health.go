package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	healthStatusOK       = "ok"
	healthStatusDegraded = "degraded"
	healthStatusDown     = "down"

	// DefaultHealthCheckTimeout bounds one dependency probe.
	DefaultHealthCheckTimeout = 3 * time.Second

	// DefaultHealthCacheTTL is how long a probe round is reused. /health is
	// unauthenticated: without the cache every hit would fan out to Postgres,
	// Solana RPC, Privy and the price API.
	DefaultHealthCacheTTL = 10 * time.Second
)

// HealthCheck probes one dependency. Critical checks take the API out of
// rotation (503) when they fail; the rest only mark it degraded, because a
// third-party blip should not make an orchestrator restart a process that is
// mid-way through moving money.
type HealthCheck struct {
	Name     string
	Critical bool
	Check    func(ctx context.Context) error
}

type healthCheckResult struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latencyMs"`
}

type healthResponse struct {
	Status    string                       `json:"status"`
	CheckedAt string                       `json:"checkedAt,omitempty"`
	Checks    map[string]healthCheckResult `json:"checks,omitempty"`
}

// HealthHandlers serves GET /health with the status of every dependency.
type HealthHandlers struct {
	Checks []HealthCheck
	// Timeout bounds one probe; zero uses DefaultHealthCheckTimeout.
	Timeout time.Duration
	// CacheTTL is how long a probe round is reused; zero uses DefaultHealthCacheTTL.
	CacheTTL time.Duration
	// Now is the clock; nil uses time.Now. Intended for tests.
	Now func() time.Time

	mu       sync.Mutex
	cached   healthResponse
	cachedAt time.Time
}

// HealthHandler serves GET /health. 200 when every critical dependency is
// reachable ("ok", or "degraded" when a non-critical one is not), 503 otherwise.
func (h *HealthHandlers) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := newRequestLog(r, "GET /health")

	resp := h.report(ctx)
	status := http.StatusOK
	if resp.Status == healthStatusDown {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
	log.done(ctx, resp.Status, status)
}

// report returns the cached probe round or runs a new one. The lock is held
// across the probes on purpose: concurrent callers wait for one round instead
// of each starting their own.
func (h *HealthHandlers) report(ctx context.Context) healthResponse {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	if !h.cachedAt.IsZero() && now.Sub(h.cachedAt) < h.cacheTTL() {
		return h.cached
	}

	// Detached from the request: a caller hanging up must not poison the round
	// that other callers are about to reuse.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.timeout())
	defer cancel()

	resp := healthResponse{
		Status:    healthStatusOK,
		CheckedAt: now.UTC().Format(time.RFC3339),
		Checks:    h.runChecks(probeCtx),
	}
	for _, check := range h.Checks {
		if resp.Checks[check.Name].Status == healthStatusOK {
			continue
		}
		if check.Critical {
			resp.Status = healthStatusDown
			break
		}
		resp.Status = healthStatusDegraded
	}

	h.cached = resp
	h.cachedAt = now
	return resp
}

func (h *HealthHandlers) runChecks(ctx context.Context) map[string]healthCheckResult {
	results := make([]healthCheckResult, len(h.Checks))
	var wg sync.WaitGroup
	for i, check := range h.Checks {
		wg.Add(1)
		go func(i int, check HealthCheck) {
			defer wg.Done()
			results[i] = h.runCheck(ctx, check)
		}(i, check)
	}
	wg.Wait()

	byName := make(map[string]healthCheckResult, len(h.Checks))
	for i, check := range h.Checks {
		byName[check.Name] = results[i]
	}
	return byName
}

func (h *HealthHandlers) runCheck(ctx context.Context, check HealthCheck) healthCheckResult {
	start := time.Now()

	// A probe that ignores ctx must not hold /health open past the timeout.
	done := make(chan error, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- fmt.Errorf("panic: %v", rec)
			}
		}()
		done <- check.Check(ctx)
	}()

	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = fmt.Errorf("timed out: %w", ctx.Err())
	}

	result := healthCheckResult{Status: healthStatusOK, LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		// The cause goes to the log only: /health is unauthenticated and probe
		// errors can quote RPC URLs that embed an API key.
		slog.WarnContext(ctx, "health check failed",
			"check", check.Name,
			"critical", check.Critical,
			"latency_ms", result.LatencyMs,
			"err", err,
		)
		result.Status = healthStatusDown
	}
	return result
}

func (h *HealthHandlers) timeout() time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return DefaultHealthCheckTimeout
}

func (h *HealthHandlers) cacheTTL() time.Duration {
	if h.CacheTTL > 0 {
		return h.CacheTTL
	}
	return DefaultHealthCacheTTL
}

func (h *HealthHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}
