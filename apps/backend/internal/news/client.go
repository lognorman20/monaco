// Package news serves headlines for the stock screen and a market pulse for the Stocks
// tab, read from keyless public RSS feeds so they cost nothing.
//
// A listed stock reads Yahoo Finance's per-symbol feed for its underlying ticker
// (GOOGLx is GOOGL). A pre-IPO token has no ticker, so it reads a Google News search
// for the company's name. The market pulse is a Google News search for the day's
// market coverage. Every feed is cached per subject for ten minutes, and a feed that
// fails serves the last list it gave rather than an error.
package news

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

const (
	// requestTimeout bounds one feed fetch. A headline list is never worth making the
	// stock screen wait longer than this for.
	requestTimeout = 8 * time.Second
	// maxBodyBytes caps what is read from a feed. A Google News search is ~130 KB.
	maxBodyBytes = 2 << 20
	// userAgent identifies us the way a feed reader does. Yahoo's feed host answers a
	// full desktop-browser user agent with 429 — it expects a browser to carry its
	// consent cookies — and serves the same feed to a "compatible" reader string.
	// Google News serves either.
	userAgent = "Mozilla/5.0 (compatible; MonacoNews/1.0; +https://trymonaco.xyz)"

	yahooFeedBaseURL  = "https://feeds.finance.yahoo.com/rss/2.0/headline"
	googleFeedBaseURL = "https://news.google.com/rss/search"

	// Metric labels for the two upstreams (telemetry.UpstreamYahooNews and
	// telemetry.UpstreamGoogleNews). Anything else, a test server, is "news".
	upstreamOther = "news"
)

// Source is one feed to read, and how to read it.
type Source struct {
	// URL is the feed.
	URL string
	// Publisher is printed on an item that names no source of its own. Empty uses the
	// channel title.
	Publisher string
	// Ranked says the feed's order is a ranking (Google News search: relevance), so its
	// top items are kept before sorting by time. A feed without it is read as a stream:
	// sorted by time first, then the newest are kept.
	Ranked bool
}

// YahooSymbolFeed is Yahoo Finance's headline feed for one equity ticker ("GOOGL").
func YahooSymbolFeed(tickers ...string) Source {
	query := url.Values{"s": {strings.Join(tickers, ",")}, "region": {"US"}, "lang": {"en-US"}}
	return Source{URL: yahooFeedBaseURL + "?" + query.Encode(), Publisher: "Yahoo Finance"}
}

// GoogleNewsSearch is a Google News search feed, in US English.
func GoogleNewsSearch(query string) Source {
	params := url.Values{"q": {query}, "hl": {"en-US"}, "gl": {"US"}, "ceid": {"US:en"}}
	return Source{URL: googleFeedBaseURL + "?" + params.Encode(), Ranked: true}
}

// Fetcher reads one feed into headlines, in the feed's order.
type Fetcher interface {
	Fetch(ctx context.Context, source Source) ([]Item, error)
}

// Client fetches feeds over HTTP.
type Client struct {
	http *http.Client
}

// NewClient builds a client with the feed timeout, instrumented like every other
// upstream. httpClient is for tests; nil uses a fresh one.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{http: &http.Client{
		Timeout:       httpClient.Timeout,
		CheckRedirect: httpClient.CheckRedirect,
		Jar:           httpClient.Jar,
		Transport:     telemetry.TransportFunc(upstreamFor, httpClient.Transport),
	}}
}

// upstreamFor names the service for the outbound-call metrics. It returns one of a
// fixed set of names, never anything taken from the request.
func upstreamFor(req *http.Request) string {
	host := strings.ToLower(req.URL.Hostname())
	switch {
	case host == "news.google.com":
		return telemetry.UpstreamGoogleNews
	case host == "yahoo.com" || strings.HasSuffix(host, ".yahoo.com"):
		return telemetry.UpstreamYahooNews
	default:
		return upstreamOther
	}
}

// Fetch implements Fetcher.
func (c *Client) Fetch(ctx context.Context, source Source) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("news feed %s: %w", feedLabel(source.URL), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("news feed %s: status %d", feedLabel(source.URL), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("news feed %s: read: %w", feedLabel(source.URL), err)
	}
	items, err := Parse(body, source.URL, source.Publisher)
	if err != nil {
		return nil, fmt.Errorf("news feed %s: %w", feedLabel(source.URL), err)
	}
	return items, nil
}

// feedLabel is the feed's host and path for an error message; the query is the
// subject, which the caller already logs.
func feedLabel(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "feed"
	}
	return parsed.Host + parsed.Path
}
