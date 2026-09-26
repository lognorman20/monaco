package news

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedFetcher answers each feed URL from a script the test sets, and counts calls.
type scriptedFetcher struct {
	mu      sync.Mutex
	answers map[string]func() ([]Item, error)
	calls   map[string]int
	// gate, when set, holds every fetch until it is closed.
	gate chan struct{}
}

func newScriptedFetcher() *scriptedFetcher {
	return &scriptedFetcher{answers: map[string]func() ([]Item, error){}, calls: map[string]int{}}
}

func (f *scriptedFetcher) on(source Source, answer func() ([]Item, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[source.URL] = answer
}

func (f *scriptedFetcher) callsTo(source Source) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[source.URL]
}

func (f *scriptedFetcher) Fetch(_ context.Context, source Source) ([]Item, error) {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	f.calls[source.URL]++
	answer := f.answers[source.URL]
	f.mu.Unlock()
	if answer == nil {
		return nil, fmt.Errorf("no answer scripted for %s", source.URL)
	}
	return answer()
}

func headlines(titles ...string) func() ([]Item, error) {
	return func() ([]Item, error) {
		items := make([]Item, 0, len(titles))
		for i, title := range titles {
			published := testNow.Add(-time.Duration(i+1) * time.Hour)
			items = append(items, Item{Title: title, URL: "https://example.com/" + fmt.Sprint(i), Source: "Reuters", PublishedAt: &published})
		}
		return items, nil
	}
}

func failing(err error) func() ([]Item, error) {
	return func() ([]Item, error) { return nil, err }
}

// testNow is just after the fixtures were captured (the last story is 22:53 UTC).
var testNow = time.Date(2026, time.September, 25, 23, 15, 0, 0, time.UTC)

// testClock is a clock the test moves by hand.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestService(fetcher Fetcher) (*Service, *testClock) {
	clock := &testClock{now: testNow}
	return NewService(fetcher).WithClock(clock.Now), clock
}

var alphabet = SubjectFor("GOOGLx", "Alphabet", false)

func TestSubjectFor(t *testing.T) {
	tests := []struct {
		name        string
		symbol      string
		catalogName string
		preIPO      bool
		wantTicker  string
		wantName    string
		wantFeeds   int
	}{
		{"a listed stock reads its underlying ticker", "GOOGLx", "Alphabet", false, "GOOGL", "Alphabet", 2},
		{"a share class maps the way the chart does", "BRK.Bx", "Berkshire Hathaway xStock", false, "BRK-B", "Berkshire Hathaway", 2},
		{"a Tessera token searches its name without the prefix", "tSpaceX", "T-SpaceX", true, "", "SpaceX", 1},
		{"a PreStocks token searches its name without the suffix", "ANTHROPIC", "Anthropic PreStocks", true, "", "Anthropic", 1},
		{"a pre-IPO token with no name searches its symbol", "tOpenAI", "", true, "", "OpenAI", 1},
		{"a stock the chart cannot map still searches its name", "SPY", "SPDR S&P 500", false, "", "SPDR S&P 500", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			subject := SubjectFor(tc.symbol, tc.catalogName, tc.preIPO)

			// Assert
			if subject.Ticker != tc.wantTicker || subject.Name != tc.wantName {
				t.Errorf("subject = %+v, want ticker %q name %q", subject, tc.wantTicker, tc.wantName)
			}
			if got := len(subject.sources()); got != tc.wantFeeds {
				t.Errorf("feeds = %d, want %d", got, tc.wantFeeds)
			}
		})
	}
}

func TestSubject_sources(t *testing.T) {
	// Act
	stock := SubjectFor("GOOGLx", "Alphabet", false).sources()
	preIPO := SubjectFor("SPACEX", "SpaceX", true).sources()

	// Assert
	if stock[0].URL != "https://feeds.finance.yahoo.com/rss/2.0/headline?lang=en-US&region=US&s=GOOGL" {
		t.Errorf("stock feed = %q", stock[0].URL)
	}
	if stock[1].URL != "https://news.google.com/rss/search?ceid=US%3Aen&gl=US&hl=en-US&q=%22Alphabet%22+stock" {
		t.Errorf("stock fallback = %q", stock[1].URL)
	}
	if preIPO[0].URL != "https://news.google.com/rss/search?ceid=US%3Aen&gl=US&hl=en-US&q=%22SpaceX%22" || !preIPO[0].Ranked {
		t.Errorf("pre-IPO feed = %+v", preIPO[0])
	}
}

func TestSubject_twoIssuersOfOneCompanyShareACacheEntry(t *testing.T) {
	// Arrange
	tessera := SubjectFor("tSpaceX", "T-SpaceX", true)
	prestocks := SubjectFor("SPACEX", "SpaceX", true)

	// Assert
	if tessera.cacheKey() != prestocks.cacheKey() {
		t.Errorf("keys %q and %q differ", tessera.cacheKey(), prestocks.cacheKey())
	}
}

func TestService_cachesForTenMinutes(t *testing.T) {
	// Arrange
	fetcher := newScriptedFetcher()
	yahoo := YahooSymbolFeed("GOOGL")
	fetcher.on(yahoo, headlines("Alphabet moved higher"))
	service, clock := newTestService(fetcher)

	// Act
	first, err := service.Asset(context.Background(), alphabet)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	clock.Advance(9 * time.Minute)
	second, _ := service.Asset(context.Background(), alphabet)
	clock.Advance(2 * time.Minute)
	third, _ := service.Asset(context.Background(), alphabet)

	// Assert
	if got := fetcher.callsTo(yahoo); got != 2 {
		t.Errorf("yahoo fetched %d times, want 2 (once, then again after ten minutes)", got)
	}
	if !second.AsOf.Equal(first.AsOf) {
		t.Errorf("a cached answer should keep its asOf: %v vs %v", second.AsOf, first.AsOf)
	}
	if !third.AsOf.Equal(testNow.Add(11 * time.Minute)) {
		t.Errorf("third asOf = %v, want the refetch time", third.AsOf)
	}
}

func TestService_failureServesTheStaleList(t *testing.T) {
	// Arrange: one good read, then Yahoo and Google both fail.
	fetcher := newScriptedFetcher()
	yahoo := YahooSymbolFeed("GOOGL")
	google := alphabet.sources()[1]
	fetcher.on(yahoo, headlines("Alphabet moved higher", "TPUs chase power"))
	service, clock := newTestService(fetcher)
	good, err := service.Asset(context.Background(), alphabet)
	if err != nil {
		t.Fatalf("good read: %v", err)
	}
	fetcher.on(yahoo, failing(errors.New("status 429")))
	fetcher.on(google, failing(errors.New("timeout")))
	clock.Advance(11 * time.Minute)

	// Act
	stale, err := service.Asset(context.Background(), alphabet)

	// Assert
	if err != nil {
		t.Fatalf("stale read: %v, want the last list", err)
	}
	if len(stale.Items) != 2 || !stale.AsOf.Equal(good.AsOf) {
		t.Errorf("stale = %d items as of %v, want the first read's 2 as of %v", len(stale.Items), stale.AsOf, good.AsOf)
	}
}

func TestService_failureWithNothingCachedIsAnError(t *testing.T) {
	// Arrange
	fetcher := newScriptedFetcher()
	fetcher.on(YahooSymbolFeed("GOOGL"), failing(errors.New("status 429")))
	fetcher.on(alphabet.sources()[1], failing(errors.New("status 503")))
	service, _ := newTestService(fetcher)

	// Act
	_, err := service.Asset(context.Background(), alphabet)

	// Assert
	if err == nil {
		t.Fatal("want an error when every feed failed and nothing is cached")
	}
}

func TestService_aFailureIsNotRetriedForAMinute(t *testing.T) {
	// Arrange
	fetcher := newScriptedFetcher()
	yahoo := YahooSymbolFeed("GOOGL")
	google := alphabet.sources()[1]
	fetcher.on(yahoo, failing(errors.New("status 429")))
	fetcher.on(google, failing(errors.New("status 503")))
	service, clock := newTestService(fetcher)
	_, _ = service.Asset(context.Background(), alphabet)

	// Act
	clock.Advance(30 * time.Second)
	_, errWithin := service.Asset(context.Background(), alphabet)
	fetcher.on(yahoo, headlines("Alphabet moved higher"))
	clock.Advance(31 * time.Second)
	after, errAfter := service.Asset(context.Background(), alphabet)

	// Assert
	if errWithin == nil {
		t.Error("within the backoff the remembered failure should answer")
	}
	if got := fetcher.callsTo(yahoo); got != 2 {
		t.Errorf("yahoo fetched %d times, want 2 (not during the backoff)", got)
	}
	if errAfter != nil || len(after.Items) != 1 {
		t.Errorf("after the backoff: %v, %d items; want the fresh list", errAfter, len(after.Items))
	}
}

func TestService_fallsBackToASearchWhenYahooFails(t *testing.T) {
	// Arrange
	fetcher := newScriptedFetcher()
	fetcher.on(YahooSymbolFeed("GOOGL"), failing(errors.New("status 429")))
	fetcher.on(alphabet.sources()[1], headlines("Alphabet wins cloud deal"))
	service, _ := newTestService(fetcher)

	// Act
	feed, err := service.Asset(context.Background(), alphabet)

	// Assert
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Title != "Alphabet wins cloud deal" {
		t.Fatalf("feed = %+v, err = %v; want the search's headline", feed, err)
	}
}

func TestService_anEmptyFeedFallsThroughButIsNotAnError(t *testing.T) {
	// Arrange: Yahoo knows nothing about the ticker, and neither does the search.
	fetcher := newScriptedFetcher()
	google := alphabet.sources()[1]
	fetcher.on(YahooSymbolFeed("GOOGL"), headlines())
	fetcher.on(google, headlines())
	service, clock := newTestService(fetcher)

	// Act
	feed, err := service.Asset(context.Background(), alphabet)
	clock.Advance(3 * time.Minute)
	_, _ = service.Asset(context.Background(), alphabet)

	// Assert
	if err != nil {
		t.Fatalf("err = %v, want an empty answer", err)
	}
	if feed.Items == nil || len(feed.Items) != 0 {
		t.Errorf("items = %#v, want an empty, non-nil list", feed.Items)
	}
	if got := fetcher.callsTo(google); got != 2 {
		t.Errorf("search fetched %d times, want 2: an empty answer is kept for two minutes, not ten", got)
	}
}

func TestService_concurrentRequestsShareOneFetch(t *testing.T) {
	// Arrange
	fetcher := newScriptedFetcher()
	fetcher.gate = make(chan struct{})
	yahoo := YahooSymbolFeed("GOOGL")
	fetcher.on(yahoo, headlines("Alphabet moved higher"))
	service, _ := newTestService(fetcher)

	// Act
	var wg sync.WaitGroup
	var answered atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if feed, err := service.Asset(context.Background(), alphabet); err == nil && len(feed.Items) == 1 {
				answered.Add(1)
			}
		}()
	}
	// Let the goroutines pile up on the one fetch before it answers.
	time.Sleep(20 * time.Millisecond)
	close(fetcher.gate)
	wg.Wait()

	// Assert
	if got := fetcher.callsTo(yahoo); got != 1 {
		t.Errorf("yahoo fetched %d times, want 1", got)
	}
	if answered.Load() != 8 {
		t.Errorf("%d of 8 callers got the list", answered.Load())
	}
}

func TestService_market(t *testing.T) {
	// Arrange: the search fails, Yahoo's index feed answers.
	fetcher := newScriptedFetcher()
	sources := MarketSources()
	fetcher.on(sources[0], failing(errors.New("status 503")))
	fetcher.on(sources[1], headlines("S&P 500 ends the week higher"))
	service, _ := newTestService(fetcher)

	// Act
	feed, err := service.Market(context.Background())

	// Assert
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("feed = %+v, err = %v", feed, err)
	}
}

func TestSelect(t *testing.T) {
	at := func(hoursAgo int) *time.Time {
		v := testNow.Add(-time.Duration(hoursAgo) * time.Hour)
		return &v
	}
	item := func(title string, published *time.Time) Item {
		return Item{Title: title, URL: "https://example.com/" + title, Source: "Reuters", PublishedAt: published}
	}
	tests := []struct {
		name   string
		items  []Item
		ranked bool
		want   []string
	}{
		{
			name:  "newest first, undated last",
			items: []Item{item("b", at(3)), item("undated", nil), item("a", at(1))},
			want:  []string{"a", "b", "undated"},
		},
		{
			name:  "a duplicate link or title is dropped",
			items: []Item{item("a", at(1)), {Title: "A", URL: "https://example.com/other", PublishedAt: at(2)}, item("a", at(3))},
			want:  []string{"a"},
		},
		{
			name:  "a date from the future is forgotten",
			items: []Item{item("future", ptr(testNow.Add(3*time.Hour))), item("a", at(1))},
			want:  []string{"a", "future"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := Select(tc.items, tc.ranked, testNow)

			// Assert
			if len(got) != len(tc.want) {
				t.Fatalf("got %d items, want %v", len(got), tc.want)
			}
			for i, title := range tc.want {
				if got[i].Title != title {
					t.Errorf("item %d = %q, want %q", i, got[i].Title, title)
				}
			}
		})
	}
}

func TestSelect_capsATwelve(t *testing.T) {
	// Arrange: twenty items, the feed's ranking putting the oldest first.
	items := make([]Item, 20)
	for i := range items {
		published := testNow.Add(-time.Duration(20-i) * time.Hour)
		items[i] = Item{Title: fmt.Sprint("story ", i), URL: fmt.Sprint("https://example.com/", i), PublishedAt: &published}
	}

	// Act
	stream := Select(items, false, testNow)
	ranked := Select(items, true, testNow)

	// Assert: a stream keeps its newest twelve; a ranking keeps its top twelve.
	if len(stream) != MaxItems || len(ranked) != MaxItems {
		t.Fatalf("stream %d, ranked %d; want %d each", len(stream), len(ranked), MaxItems)
	}
	if stream[0].Title != "story 19" || stream[MaxItems-1].Title != "story 8" {
		t.Errorf("stream = %q … %q, want the newest twelve", stream[0].Title, stream[MaxItems-1].Title)
	}
	if ranked[0].Title != "story 11" || ranked[MaxItems-1].Title != "story 0" {
		t.Errorf("ranked = %q … %q, want the feed's top twelve, newest first", ranked[0].Title, ranked[MaxItems-1].Title)
	}
}

func ptr(v time.Time) *time.Time { return &v }
