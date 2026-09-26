package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_fetchesAndParsesAFeed(t *testing.T) {
	// Arrange: a server that answers like Yahoo does — the feed for a reader's user
	// agent, 429 for a browser's.
	body := readFixture(t, "yahoo_googl.xml")
	var gotUA, gotSymbol string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotSymbol = r.UserAgent(), r.URL.Query().Get("s")
		if strings.Contains(gotUA, "Safari") {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	source := Source{URL: server.URL + "/rss/2.0/headline?s=GOOGL", Publisher: "Yahoo Finance"}

	// Act
	items, err := NewClient(server.Client()).Fetch(context.Background(), source)

	// Assert
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(items) != 18 {
		t.Errorf("items = %d, want 18", len(items))
	}
	if gotSymbol != "GOOGL" || !strings.Contains(gotUA, "MonacoNews") {
		t.Errorf("request s=%q ua=%q", gotSymbol, gotUA)
	}
}

func TestClient_errors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"a non-200 status", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		}},
		{"a body that is not a feed", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html><body>consent</body"))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			// Act
			_, err := NewClient(server.Client()).Fetch(context.Background(), Source{URL: server.URL})

			// Assert
			if err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestClient_givesUpOnASlowFeed(t *testing.T) {
	// Arrange: a feed that never answers inside the caller's deadline.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Act
	start := time.Now()
	_, err := NewClient(server.Client()).Fetch(ctx, Source{URL: server.URL})

	// Assert
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v, want the deadline honoured", elapsed)
	}
}

func TestService_overHTTP_servesTheLastListWhenTheFeedGoesDown(t *testing.T) {
	// Arrange: the real client and cache against a server that answers once and then
	// starts refusing.
	body := readFixture(t, "google_spacex.xml")
	var down atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	fetcher := redirectFetcher{client: NewClient(server.Client()), to: server.URL}
	service, clock := newTestService(fetcher)
	spacex := SubjectFor("tSpaceX", "T-SpaceX", true)

	// Act
	first, err := service.Asset(context.Background(), spacex)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	down.Store(true)
	clock.Advance(11 * time.Minute)
	second, err := service.Asset(context.Background(), spacex)

	// Assert
	if err != nil {
		t.Fatalf("second: %v, want the stale list", err)
	}
	if len(first.Items) != MaxItems || len(second.Items) != MaxItems {
		t.Errorf("first %d, second %d items; want %d each", len(first.Items), len(second.Items), MaxItems)
	}
	if first.Items[0].Title != "Dow Jones Futures: Growth Stocks Shrug Off Surging Yields; Micron, SpaceX, Tesla, Key Economic Data Due" {
		t.Errorf("newest = %q", first.Items[0].Title)
	}
}

// redirectFetcher sends every source to one test server, keeping its query.
type redirectFetcher struct {
	client *Client
	to     string
}

func (f redirectFetcher) Fetch(ctx context.Context, source Source) ([]Item, error) {
	query := ""
	if i := strings.IndexByte(source.URL, '?'); i >= 0 {
		query = source.URL[i:]
	}
	source.URL = f.to + "/feed" + query
	return f.client.Fetch(ctx, source)
}
