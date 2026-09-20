package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

// staleAfterIntervals is how many missed intervals mark a poller stale. One slow tick is
// normal (a sweep waits on Solana confirmation); three in a row is a stuck goroutine.
const staleAfterIntervals = 3

// minStaleAfter floors the stale window so a short interval does not flap on one slow tick.
const minStaleAfter = 2 * time.Minute

var pollers = &pollerRegistry{entries: make(map[string]*pollerEntry)}

type pollerEntry struct {
	interval time.Duration
	lastTick time.Time
}

type pollerRegistry struct {
	mu      sync.Mutex
	entries map[string]*pollerEntry
	now     func() time.Time
}

func (r *pollerRegistry) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// RegisterPoller declares a poller that is expected to tick every interval. Registration
// counts as a tick, so a freshly booted API is not reported stale before its first tick.
func RegisterPoller(name string, interval time.Duration) {
	pollers.mu.Lock()
	defer pollers.mu.Unlock()
	pollers.entries[name] = &pollerEntry{interval: interval, lastTick: pollers.clock()}
	pollerLastTick.WithLabelValues(name).Set(float64(pollers.clock().Unix()))
}

// GuardTick runs one poller tick. It records duration, outcome and the heartbeat, and it
// turns a panic into a logged, alerted, counted failure instead of a dead process: the
// pollers share the API process, so an unrecovered panic in one takes every route down.
func GuardTick(ctx context.Context, name string, tick func() error) {
	start := time.Now()
	outcome := OutcomeOK
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = OutcomePanic
			stack := debug.Stack()
			slog.ErrorContext(ctx, "poller tick panicked",
				"poller", name,
				"panic", fmt.Sprint(recovered),
				"stack", string(stack),
			)
			CapturePanic(recovered, stack, map[string]string{"poller": name})
			Alert(ctx, AlertEvent{
				Kind:     "poller_panic",
				Key:      "poller_panic:" + name,
				Severity: SeverityCritical,
				Title:    "Poller " + name + " panicked",
				Detail:   fmt.Sprint(recovered),
			})
		}
		elapsed := time.Since(start)
		pollerTicks.WithLabelValues(name, outcome).Inc()
		pollerTickDuration.WithLabelValues(name).Observe(elapsed.Seconds())
		heartbeat(name)
	}()

	if err := tick(); err != nil && ctx.Err() == nil {
		outcome = OutcomeError
	}
}

func heartbeat(name string) {
	pollers.mu.Lock()
	defer pollers.mu.Unlock()
	now := pollers.clock()
	entry, ok := pollers.entries[name]
	if !ok {
		entry = &pollerEntry{}
		pollers.entries[name] = entry
	}
	entry.lastTick = now
	pollerLastTick.WithLabelValues(name).Set(float64(now.Unix()))
}

// StalePollers lists registered pollers that have not finished a tick within their stale
// window, sorted by name. A panicking tick still counts as a heartbeat: the loop is alive,
// and the panic has its own alert.
func StalePollers() []string {
	pollers.mu.Lock()
	defer pollers.mu.Unlock()
	now := pollers.clock()
	var stale []string
	for name, entry := range pollers.entries {
		window := time.Duration(staleAfterIntervals) * entry.interval
		if window < minStaleAfter {
			window = minStaleAfter
		}
		if now.Sub(entry.lastTick) > window {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	return stale
}

// CheckPollers is a health probe: it fails when any registered poller is stale.
func CheckPollers(context.Context) error {
	if stale := StalePollers(); len(stale) > 0 {
		return fmt.Errorf("stale pollers: %v", stale)
	}
	return nil
}
