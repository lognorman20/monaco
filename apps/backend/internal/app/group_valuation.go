package app

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// groupValuation is a group's live pot value and net USDC in, read together so
// P&L (pot - net in) is never computed from a pot and a net-in taken at
// different moments.
type groupValuation struct {
	PotNavMicros    int64
	NetUsdcInMicros int64
	ValuedAt        time.Time
	// Degraded is true when marking the pot failed and PotNavMicros fell back
	// to net USDC in (P&L reads as zero). Degraded values are never cached.
	Degraded bool
}

func (v groupValuation) DollarPnLMicros() int64 {
	return v.PotNavMicros - v.NetUsdcInMicros
}

// groupValuationTTL bounds how stale a discovery row can be. Valuing a pot
// costs a treasury balance read and a Pyth mark per group; the leaderboard
// values every funded group, so without a cache each Groups tab open would fan
// out to every treasury on the platform.
const groupValuationTTL = 30 * time.Second

// groupValuationWorkers caps concurrent pot valuations for one request.
const groupValuationWorkers = 6

type groupValuationCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]groupValuation
}

func newGroupValuationCache(ttl time.Duration) *groupValuationCache {
	return &groupValuationCache{ttl: ttl, entries: make(map[string]groupValuation)}
}

// get returns a cached valuation taken no earlier than notBefore and within TTL of now.
func (c *groupValuationCache) get(groupID string, now, notBefore time.Time) (groupValuation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[groupID]
	if !ok {
		return groupValuation{}, false
	}
	if now.Sub(v.ValuedAt) > c.ttl || v.ValuedAt.Before(notBefore) {
		delete(c.entries, groupID)
		return groupValuation{}, false
	}
	return v, true
}

func (c *groupValuationCache) put(groupID string, v groupValuation) {
	if v.Degraded {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[groupID] = v
}

// valueGroup computes live pot NAV and net USDC in exactly as GET /v1/home
// does for its group board (groupNetUsdcIn + groupPotNavAndShares), so the
// Groups tab and Home rank the same numbers.
func (h *HomeService) valueGroup(ctx context.Context, groupID string, now time.Time) (groupValuation, error) {
	netUsdcIn, err := h.groupNetUsdcIn(ctx, groupID)
	if err != nil {
		return groupValuation{}, err
	}
	potNav, _, err := h.groupPotNavAndShares(ctx, groupID, netUsdcIn)
	if err != nil {
		if ctx.Err() != nil {
			return groupValuation{}, ctx.Err()
		}
		// Same fallback GET /v1/home uses for discovery rows: one unpriceable
		// pot must not take down a platform-wide list.
		slog.Warn("group valuation failed; using net usdc in", "group_id", groupID, "err", err)
		return groupValuation{
			PotNavMicros:    netUsdcIn,
			NetUsdcInMicros: netUsdcIn,
			ValuedAt:        now,
			Degraded:        true,
		}, nil
	}
	return groupValuation{
		PotNavMicros:    potNav,
		NetUsdcInMicros: netUsdcIn,
		ValuedAt:        now,
	}, nil
}

// valueGroups returns valuations for groupIDs, serving fresh cache hits and
// computing misses with bounded concurrency. notBefore maps a group id to the
// earliest acceptable ValuedAt (e.g. its latest NAV snapshot); missing keys
// accept any fresh entry.
func (s *GroupsTabService) valueGroups(ctx context.Context, groupIDs []string, notBefore map[string]time.Time) (map[string]groupValuation, error) {
	now := s.now().UTC()
	out := make(map[string]groupValuation, len(groupIDs))
	var misses []string
	for _, id := range groupIDs {
		if _, dup := out[id]; dup {
			continue
		}
		if v, ok := s.valuations.get(id, now, notBefore[id]); ok {
			out[id] = v
			continue
		}
		misses = append(misses, id)
	}
	if len(misses) == 0 {
		return out, nil
	}

	type result struct {
		id  string
		v   groupValuation
		err error
	}
	jobs := make(chan string)
	results := make(chan result, len(misses))
	workers := groupValuationWorkers
	if len(misses) < workers {
		workers = len(misses)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				v, err := s.home.valueGroup(ctx, id, s.now().UTC())
				results <- result{id: id, v: v, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, id := range misses {
			select {
			case jobs <- id:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		s.valuations.put(r.id, r.v)
		out[r.id] = r.v
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
