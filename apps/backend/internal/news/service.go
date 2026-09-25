package news

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/yahoocharts"
)

const (
	// MaxItems is the most headlines one feed answers with.
	MaxItems = 12
	// DefaultTTL is how long a feed's list is reused. Headlines move in hours, not
	// seconds, and every stock screen asks on open.
	DefaultTTL = 10 * time.Minute
	// emptyTTL is how long "this feed has nothing" is reused: long enough that a quiet
	// ticker does not go upstream on every open, short enough that its first story
	// shows up soon after it is written.
	emptyTTL = 2 * time.Minute
	// failureBackoff is how long a failed fetch is remembered before the next request
	// may try again. Without it a feed that is down (or rate-limiting us) would be
	// asked again by every screen that opens, each waiting out the timeout.
	failureBackoff = time.Minute
	// futureSlack is how far ahead of now a timestamp may be before it is treated as
	// wrong. A feed with a broken clock would otherwise pin its story to the top.
	futureSlack = 15 * time.Minute

	marketCacheKey = "market"
)

// ErrNoSource means a subject had nothing to search for: no ticker and no name.
var ErrNoSource = errors.New("news: nothing to search for")

// Feed is one answer: the headlines, newest first, and when they were fetched.
type Feed struct {
	Items []Item
	// AsOf is when this list came from upstream. A list served stale after a failed
	// fetch keeps the time it was actually read.
	AsOf time.Time
}

// Subject is what one stock's headlines are about.
type Subject struct {
	// Ticker is the equity ticker Yahoo knows ("GOOGL"). Empty for a pre-IPO token.
	Ticker string
	// Name is the company, for a search ("Alphabet", "SpaceX").
	Name string
	// PreIPO marks a token with no listed equity behind it.
	PreIPO bool
}

// SubjectFor builds the subject for a catalog asset. The ticker comes from the same
// mapping the charts use, so the headlines are about the stock the curve draws.
func SubjectFor(symbol, catalogName string, preIPO bool) Subject {
	name := displayName(catalogName)
	if name == "" && preIPO {
		// A pre-IPO symbol is a company name in capitals (SPACEX); it still searches.
		name = strings.TrimPrefix(strings.TrimSpace(symbol), "t")
	}
	subject := Subject{Name: name, PreIPO: preIPO}
	if !preIPO {
		subject.Ticker = yahoocharts.UnderlyingTicker(symbol)
	}
	return subject
}

// displayName drops the issuers' branding from a catalog name, the way the app does:
// "T-SpaceX" and "SpaceX PreStocks" are SpaceX, "Apple xStock" is Apple.
func displayName(catalogName string) string {
	name := strings.TrimSpace(catalogName)
	if len(name) > 2 && strings.EqualFold(name[:2], "T-") {
		name = name[2:]
	}
	lower := strings.ToLower(name)
	for _, suffix := range []string{" prestocks", " xstocks", " xstock"} {
		if strings.HasSuffix(lower, suffix) {
			name = name[:len(name)-len(suffix)]
			break
		}
	}
	return strings.TrimSpace(name)
}

// cacheKey is per company, not per symbol: tSpaceX and SPACEX are two issuers of one
// SpaceX, and share one list.
func (s Subject) cacheKey() string {
	if s.Ticker != "" {
		return "ticker:" + strings.ToUpper(s.Ticker)
	}
	return "name:" + strings.ToLower(s.Name)
}

// sources is the feeds to try, in order. A listed stock reads Yahoo's feed for its
// ticker and falls back to a search for its name; a pre-IPO token only has its name.
func (s Subject) sources() []Source {
	var sources []Source
	if s.Ticker != "" {
		sources = append(sources, YahooSymbolFeed(s.Ticker))
	}
	if s.Name != "" {
		query := `"` + s.Name + `"`
		if !s.PreIPO {
			// "Apple" alone is fruit as often as it is a company.
			query += " stock"
		}
		sources = append(sources, GoogleNewsSearch(query))
	}
	return sources
}

// MarketSources is the feed behind the Stocks tab's market pulse: the day's coverage
// of the US market, then Yahoo's index feed if Google cannot answer.
//
// Google first on purpose. Yahoo's S&P 500 / Nasdaq feed is mostly one-stock filler
// ("PACB Beats Stock Market Upswing"), where a search for the day's market coverage
// ranks Reuters, CNBC and the Journal first.
func MarketSources() []Source {
	return []Source{
		GoogleNewsSearch(`"stock market" OR "Wall Street" when:1d`),
		YahooSymbolFeed("^GSPC", "^IXIC"),
	}
}

type cacheEntry struct {
	feed       Feed
	hasFeed    bool
	freshUntil time.Time
	// retryAfter and err remember the last failed fetch.
	retryAfter time.Time
	err        error
}

type inflight struct {
	done chan struct{}
	feed Feed
	err  error
}

// Service answers news requests from a per-subject cache over a Fetcher.
type Service struct {
	fetcher Fetcher
	ttl     time.Duration
	now     func() time.Time

	mu       sync.Mutex
	entries  map[string]cacheEntry
	inflight map[string]*inflight
}

// NewService caches fetcher's answers for DefaultTTL.
func NewService(fetcher Fetcher) *Service {
	return &Service{
		fetcher:  fetcher,
		ttl:      DefaultTTL,
		now:      time.Now,
		entries:  make(map[string]cacheEntry),
		inflight: make(map[string]*inflight),
	}
}

// WithClock pins the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Asset answers the headlines for one stock.
func (s *Service) Asset(ctx context.Context, subject Subject) (Feed, error) {
	sources := subject.sources()
	if len(sources) == 0 {
		return Feed{}, ErrNoSource
	}
	return s.get(ctx, subject.cacheKey(), sources)
}

// Market answers the Stocks tab's market pulse.
func (s *Service) Market(ctx context.Context) (Feed, error) {
	return s.get(ctx, marketCacheKey, MarketSources())
}

// get serves key from the cache, or fetches it once for however many callers are
// waiting. A fetch that fails serves the last good list if there is one.
func (s *Service) get(ctx context.Context, key string, sources []Source) (Feed, error) {
	now := s.now()
	s.mu.Lock()
	entry, cached := s.entries[key]
	if cached && entry.hasFeed && now.Before(entry.freshUntil) {
		s.mu.Unlock()
		return entry.feed, nil
	}
	if cached && now.Before(entry.retryAfter) {
		s.mu.Unlock()
		if entry.hasFeed {
			return entry.feed, nil
		}
		return Feed{}, entry.err
	}
	if call, running := s.inflight[key]; running {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.feed, call.err
		case <-ctx.Done():
			return Feed{}, ctx.Err()
		}
	}
	call := &inflight{done: make(chan struct{})}
	s.inflight[key] = call
	s.mu.Unlock()

	// The fetch is shared, so one caller hanging up must not cancel it for the rest.
	// Each feed request carries its own timeout.
	feed, err := s.fetch(context.WithoutCancel(ctx), sources)

	s.mu.Lock()
	delete(s.inflight, key)
	entry = s.entries[key]
	if err != nil {
		entry.retryAfter = now.Add(failureBackoff)
		entry.err = err
		if entry.hasFeed {
			slog.WarnContext(ctx, "news feed failed; serving the last list", "key", key, "as_of", entry.feed.AsOf, "err", err)
			feed, err = entry.feed, nil
		} else {
			slog.WarnContext(ctx, "news feed failed", "key", key, "err", err)
		}
	} else {
		ttl := s.ttl
		if len(feed.Items) == 0 {
			ttl = min(emptyTTL, s.ttl)
		}
		entry = cacheEntry{feed: feed, hasFeed: true, freshUntil: now.Add(ttl)}
	}
	s.entries[key] = entry
	call.feed, call.err = feed, err
	s.mu.Unlock()
	close(call.done)
	return feed, err
}

// fetch walks sources in order and answers with the first that has headlines. A feed
// that answers with none is not a failure, but the next feed may still know better;
// only when every feed failed is the whole fetch a failure.
func (s *Service) fetch(ctx context.Context, sources []Source) (Feed, error) {
	var lastErr error
	answered := false
	for _, source := range sources {
		items, err := s.fetcher.Fetch(ctx, source)
		if err != nil {
			lastErr = err
			slog.WarnContext(ctx, "news source failed", "feed", feedLabel(source.URL), "err", err)
			continue
		}
		answered = true
		if picked := Select(items, source.Ranked, s.now()); len(picked) > 0 {
			return Feed{Items: picked, AsOf: s.now().UTC()}, nil
		}
	}
	if answered {
		return Feed{Items: []Item{}, AsOf: s.now().UTC()}, nil
	}
	return Feed{}, lastErr
}

// Select turns a feed's items into what the app shows: duplicates dropped, a date from
// the future forgotten, at most MaxItems, newest first with undated items last.
//
// A ranked feed keeps its own top MaxItems before sorting, so a search's best stories
// survive; a stream sorts first, so its newest do.
func Select(items []Item, ranked bool, now time.Time) []Item {
	seenURL := make(map[string]bool, len(items))
	seenTitle := make(map[string]bool, len(items))
	picked := make([]Item, 0, min(len(items), MaxItems))
	for _, item := range items {
		titleKey := strings.ToLower(strings.Join(strings.Fields(item.Title), " "))
		if seenURL[item.URL] || seenTitle[titleKey] {
			continue
		}
		seenURL[item.URL], seenTitle[titleKey] = true, true
		if item.PublishedAt != nil && item.PublishedAt.After(now.Add(futureSlack)) {
			item.PublishedAt = nil
		}
		picked = append(picked, item)
	}
	if ranked && len(picked) > MaxItems {
		picked = picked[:MaxItems]
	}
	sort.SliceStable(picked, func(i, j int) bool {
		a, b := picked[i].PublishedAt, picked[j].PublishedAt
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return a.After(*b)
		}
	})
	if len(picked) > MaxItems {
		picked = picked[:MaxItems]
	}
	return picked
}
