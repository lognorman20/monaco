package news

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func TestParse_yahooSymbolFeed(t *testing.T) {
	// Arrange: Yahoo's GOOGL feed as captured on 2026-09-25. Its items carry no
	// <source>, and one links off to stocktwits.com.
	body := readFixture(t, "yahoo_googl.xml")

	// Act
	items, err := Parse(body, YahooSymbolFeed("GOOGL").URL, "Yahoo Finance")

	// Assert
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 18 {
		t.Fatalf("items = %d, want 18", len(items))
	}
	first := items[0]
	if first.Title != "What Will the Q3 Earnings Season Show?" {
		t.Errorf("title = %q", first.Title)
	}
	if first.URL != "https://finance.yahoo.com/markets/stocks/articles/q3-earnings-season-show-205800453.html" {
		t.Errorf("url = %q, want the article without ?.tsrc=rss", first.URL)
	}
	if first.Source != "Yahoo Finance" {
		t.Errorf("source = %q, want the feed's publisher", first.Source)
	}
	want := time.Date(2026, time.September, 25, 20, 58, 0, 0, time.UTC)
	if first.PublishedAt == nil || !first.PublishedAt.Equal(want) {
		t.Errorf("publishedAt = %v, want %v", first.PublishedAt, want)
	}
	for _, item := range items {
		if strings.Contains(item.URL, "tsrc") {
			t.Errorf("tracking parameter left on %q", item.URL)
		}
	}
}

func TestParse_yahooMarketFeed_namesAnOffSiteArticleByItsHost(t *testing.T) {
	// Arrange: the index feed's first story is on stocktwits.com, not Yahoo.
	body := readFixture(t, "yahoo_market.xml")

	// Act
	items, err := Parse(body, YahooSymbolFeed("^GSPC", "^IXIC").URL, "Yahoo Finance")

	// Assert
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.HasPrefix(items[0].URL, "https://stocktwits.com/") {
		t.Fatalf("first url = %q, want the stocktwits story", items[0].URL)
	}
	if items[0].Source != "stocktwits.com" {
		t.Errorf("source = %q, want the article's own site", items[0].Source)
	}
	if !strings.HasPrefix(items[0].Title, "S&P 500, Dow, Nasdaq End Week Higher") {
		t.Errorf("title = %q, want the &amp; decoded", items[0].Title)
	}
}

func TestParse_googleNewsSearch(t *testing.T) {
	// Arrange: a Google News search for "SpaceX" as captured on 2026-09-25.
	body := readFixture(t, "google_spacex.xml")

	// Act
	items, err := Parse(body, GoogleNewsSearch(`"SpaceX"`).URL, "")

	// Assert
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 16 {
		t.Fatalf("items = %d, want 16", len(items))
	}
	first := items[0]
	if first.Title != "Elon Musk Makes Massive Nvidia Move at SpaceX" {
		t.Errorf("title = %q, want the \" - Yahoo Finance\" suffix gone", first.Title)
	}
	if first.Source != "Yahoo Finance" {
		t.Errorf("source = %q, want the <source> element", first.Source)
	}
	if strings.Contains(first.URL, "oc=5") || !strings.HasPrefix(first.URL, "https://news.google.com/rss/articles/") {
		t.Errorf("url = %q, want the article link without Google's oc tag", first.URL)
	}
	want := time.Date(2026, time.September, 25, 13, 1, 12, 0, time.UTC)
	if first.PublishedAt == nil || !first.PublishedAt.Equal(want) {
		t.Errorf("publishedAt = %v, want %v (GMT)", first.PublishedAt, want)
	}
}

func TestParse_edgeCases(t *testing.T) {
	const feedURL = "https://example.com/feeds/markets.xml"
	tests := []struct {
		name      string
		item      string
		channel   string
		wantCount int
		check     func(t *testing.T, item Item)
	}{
		{
			name:      "missing pubDate keeps the item undated",
			item:      `<title>Fed holds rates</title><link>https://example.com/a</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.PublishedAt != nil {
					t.Errorf("publishedAt = %v, want nil", item.PublishedAt)
				}
			},
		},
		{
			name:      "unparseable pubDate keeps the item undated",
			item:      `<title>Fed holds rates</title><link>https://example.com/a</link><pubDate>yesterday-ish</pubDate>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.PublishedAt != nil {
					t.Errorf("publishedAt = %v, want nil", item.PublishedAt)
				}
			},
		},
		{
			name:      "dc:date stands in for pubDate",
			item:      `<title>Fed holds rates</title><link>https://example.com/a</link><dc:date>2026-09-25T14:30:00-04:00</dc:date>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				want := time.Date(2026, time.September, 25, 18, 30, 0, 0, time.UTC)
				if item.PublishedAt == nil || !item.PublishedAt.Equal(want) {
					t.Errorf("publishedAt = %v, want %v", item.PublishedAt, want)
				}
			},
		},
		{
			name:      "an Eastern zone name is read as Eastern",
			item:      `<title>Close</title><link>https://example.com/a</link><pubDate>Fri, 25 Sep 2026 16:05:00 EDT</pubDate>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				want := time.Date(2026, time.September, 25, 20, 5, 0, 0, time.UTC)
				if item.PublishedAt == nil || !item.PublishedAt.Equal(want) {
					t.Errorf("publishedAt = %v, want %v", item.PublishedAt, want)
				}
			},
		},
		{
			name:      "a one-digit day parses",
			item:      `<title>Close</title><link>https://example.com/a</link><pubDate>Tue, 1 Sep 2026 09:00:00 +0000</pubDate>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				want := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
				if item.PublishedAt == nil || !item.PublishedAt.Equal(want) {
					t.Errorf("publishedAt = %v, want %v", item.PublishedAt, want)
				}
			},
		},
		{
			name:      "CDATA title with markup and entities",
			item:      `<title><![CDATA[<b>Nvidia</b> &amp; AMD   rally &#8212; again]]></title><link>https://example.com/a</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.Title != "Nvidia & AMD rally — again" {
					t.Errorf("title = %q", item.Title)
				}
			},
		},
		{
			name:      "an undeclared HTML entity does not fail the feed",
			item:      `<title>Stocks&nbsp;rise</title><link>https://example.com/a</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.Title != "Stocks rise" {
					t.Errorf("title = %q", item.Title)
				}
			},
		},
		{
			name:      "a relative link resolves against the channel link",
			channel:   `<link>https://news.example.org/markets/</link>`,
			item:      `<title>Oil slides</title><link>/2026/09/25/oil-slides?utm_source=rss&amp;id=7</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.URL != "https://news.example.org/2026/09/25/oil-slides?id=7" {
					t.Errorf("url = %q", item.URL)
				}
			},
		},
		{
			name:      "a relative link resolves against the feed when the channel has none",
			item:      `<title>Oil slides</title><link>oil-slides.html</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.URL != "https://example.com/feeds/oil-slides.html" {
					t.Errorf("url = %q", item.URL)
				}
			},
		},
		{
			name:      "a permalink guid stands in for a missing link",
			item:      `<title>Oil slides</title><guid>https://example.com/oil</guid>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.URL != "https://example.com/oil" {
					t.Errorf("url = %q", item.URL)
				}
			},
		},
		{
			name:      "an item without a link is dropped",
			item:      `<title>Oil slides</title><guid isPermaLink="false">abc-123</guid>`,
			wantCount: 0,
		},
		{
			name:      "a javascript link is dropped",
			item:      `<title>Oil slides</title><link>javascript:alert(1)</link>`,
			wantCount: 0,
		},
		{
			name:      "an item without a title is dropped",
			item:      `<title>  </title><link>https://example.com/a</link>`,
			wantCount: 0,
		},
		{
			name:      "the channel title names a source when the feed has no publisher",
			channel:   `<title>Example Markets</title>`,
			item:      `<title>Oil slides</title><link>https://example.com/a</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if item.Source != "Example Markets" {
					t.Errorf("source = %q", item.Source)
				}
			},
		},
		{
			name:      "an absurd title is cut",
			item:      `<title>` + strings.Repeat("word ", 200) + `</title><link>https://example.com/a</link>`,
			wantCount: 1,
			check: func(t *testing.T, item Item) {
				if n := len([]rune(item.Title)); n > maxTitleRunes {
					t.Errorf("title is %d runes, want at most %d", n, maxTitleRunes)
				}
				if !strings.HasSuffix(item.Title, "…") {
					t.Errorf("title %q should end with an ellipsis", item.Title)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			body := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel>` + tc.channel +
				`<item>` + tc.item + `</item></channel></rss>`

			// Act
			items, err := Parse([]byte(body), feedURL, "")

			// Assert
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(items) != tc.wantCount {
				t.Fatalf("items = %d, want %d: %+v", len(items), tc.wantCount, items)
			}
			if tc.check != nil {
				tc.check(t, items[0])
			}
		})
	}
}

func TestParse_latin1Charset(t *testing.T) {
	// Arrange: "Café" in ISO-8859-1 is 43 61 66 E9.
	body := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss><channel><item><title>Caf`), 0xE9)
	body = append(body, []byte(` chains</title><link>https://example.com/a</link></item></channel></rss>`)...)

	// Act
	items, err := Parse(body, "https://example.com/feed", "")

	// Assert
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 1 || items[0].Title != "Café chains" {
		t.Fatalf("items = %+v, want one titled \"Café chains\"", items)
	}
}

func TestParse_notXML(t *testing.T) {
	// Arrange: what Yahoo sends a browser user agent.
	body := []byte("Too Many Requests")

	// Act
	items, err := Parse(body, YahooSymbolFeed("GOOGL").URL, "Yahoo Finance")

	// Assert: not a feed, and not silently an empty one either.
	if err == nil {
		t.Fatalf("Parse succeeded with %d items, want an error", len(items))
	}
}
