package mintinfo

import (
	"sync"
	"time"
)

const (
	mintCacheTTL  = 10 * time.Minute
	lastGoodTTL   = 24 * time.Hour
	epochCacheTTL = 10 * time.Minute
)

type mintCacheEntry struct {
	info                    Info
	cachedAt                time.Time
	multiplierEffectiveTs   int64
	usedNewMultiplierAtCache bool
}

type lastGoodEntry struct {
	info     Info
	storedAt time.Time
}

type epochCacheEntry struct {
	epoch    uint64
	cachedAt time.Time
}

type cacheStore struct {
	mu sync.Mutex

	mints    map[string]mintCacheEntry
	lastGood map[string]lastGoodEntry
	epoch    epochCacheEntry
	now      func() time.Time
}

func newCacheStore(now func() time.Time) *cacheStore {
	return &cacheStore{
		mints:    make(map[string]mintCacheEntry),
		lastGood: make(map[string]lastGoodEntry),
		now:      now,
	}
}

func (s *cacheStore) getCached(mint string) (Info, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.mints[mint]
	if !ok {
		return Info{}, false
	}
	if now.Sub(entry.cachedAt) >= mintCacheTTL {
		return Info{}, false
	}
	if entry.multiplierEffectiveTs != 0 && now.Unix() >= entry.multiplierEffectiveTs && !entry.usedNewMultiplierAtCache {
		return Info{}, false
	}
	return entry.info, true
}

func (s *cacheStore) putMint(mint string, info Info, effectiveTs int64, usedNewMult bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mints[mint] = mintCacheEntry{
		info:                     info,
		cachedAt:                 now,
		multiplierEffectiveTs:    effectiveTs,
		usedNewMultiplierAtCache: usedNewMult,
	}
	s.lastGood[mint] = lastGoodEntry{info: info, storedAt: now}
}

func (s *cacheStore) getLastGood(mint string) (Info, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.lastGood[mint]
	if !ok || now.Sub(entry.storedAt) >= lastGoodTTL {
		return Info{}, false
	}
	return entry.info, true
}

func (s *cacheStore) getEpoch() (uint64, bool) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch.cachedAt.IsZero() || now.Sub(s.epoch.cachedAt) >= epochCacheTTL {
		return 0, false
	}
	return s.epoch.epoch, true
}

func (s *cacheStore) putEpoch(epoch uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch = epochCacheEntry{epoch: epoch, cachedAt: s.now()}
}

func mergePauseFailOpen(fetched Info, lastGood Info, hasLastGood bool) Info {
	if hasLastGood {
		fetched.Paused = lastGood.Paused
	} else {
		fetched.Paused = false
	}
	return fetched
}

func applyStaticIfZero(info Info, mint string, at time.Time) Info {
	if info.TransferFeeBps > 0 && info.UiMultiplier != nil {
		return info
	}
	static, ok := StaticFallback(mint, at)
	if !ok {
		return info
	}
	if info.TransferFeeBps <= 0 {
		info.TransferFeeBps = static.TransferFeeBps
	}
	if info.UiMultiplier == nil {
		info.UiMultiplier = static.UiMultiplier
	}
	return info
}
