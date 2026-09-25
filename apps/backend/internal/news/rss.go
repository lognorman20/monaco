package news

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
)

const (
	// maxTitleRunes bounds a headline. The app shows two lines; anything past this is
	// a feed that put the article body in the title.
	maxTitleRunes = 300
	// maxSourceRunes bounds a publisher name for the same reason.
	maxSourceRunes = 80
)

// Item is one headline as the app shows it.
type Item struct {
	Title  string
	URL    string
	Source string
	// PublishedAt is nil when the feed gave no date, or one that would not parse.
	// Such an item is still news; it sorts after every dated one.
	PublishedAt *time.Time
}

type rssDocument struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title string `xml:"title"`
	// Links is a slice because a channel can carry an <atom:link href="…"/> beside its
	// <link>, and both match this local name. A plain string would keep whichever came
	// last, which is usually the empty atom one.
	Links []string  `xml:"link"`
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string    `xml:"title"`
	Links   []string  `xml:"link"`
	GUID    rssGUID   `xml:"guid"`
	PubDate string    `xml:"pubDate"`
	DCDate  string    `xml:"http://purl.org/dc/elements/1.1/ date"`
	Source  rssSource `xml:"source"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink string `xml:"isPermaLink,attr"`
}

type rssSource struct {
	Name string `xml:",chardata"`
	URL  string `xml:"url,attr"`
}

// Parse reads an RSS 2.0 document into headlines, in the feed's own order.
//
// feedURL resolves relative links and names the feed's own site. publisher is the
// source to print on an item that names none; empty falls back to the channel title.
//
// It is lenient where real feeds are sloppy (HTML entities in the XML, a Latin-1
// charset, CDATA titles with markup in them) and strict about what reaches the app:
// an item with no title or no http(s) link is dropped rather than shown as a dead row.
func Parse(body []byte, feedURL, publisher string) ([]Item, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	// RSS in the wild carries &nbsp; and friends without declaring them. Not
	// xml.HTMLAutoClose, though: it treats <link> as HTML's void element and would
	// throw away every item's link.
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	decoder.CharsetReader = charsetReader

	var doc rssDocument
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}

	base, _ := url.Parse(feedURL)
	if home := firstAbsoluteLink(doc.Channel.Links); home != nil {
		base = home
	}
	fallbackSource := cleanText(publisher, maxSourceRunes)
	if fallbackSource == "" {
		fallbackSource = cleanText(doc.Channel.Title, maxSourceRunes)
	}

	items := make([]Item, 0, len(doc.Channel.Items))
	for _, raw := range doc.Channel.Items {
		link := itemLink(raw)
		articleURL, ok := cleanArticleURL(link, base)
		if !ok {
			continue
		}
		source := cleanText(raw.Source.Name, maxSourceRunes)
		if source == "" {
			source = offSiteSource(articleURL, base, fallbackSource)
		}
		title := cleanTitle(raw.Title, source)
		if title == "" {
			continue
		}
		item := Item{Title: title, URL: articleURL, Source: source}
		if published, ok := parseDate(raw.PubDate); ok {
			item.PublishedAt = &published
		} else if published, ok := parseDate(raw.DCDate); ok {
			item.PublishedAt = &published
		}
		items = append(items, item)
	}
	return items, nil
}

// itemLink is the item's <link>, or its guid when the guid is a permalink (RSS 2.0
// says a guid is a permalink unless isPermaLink="false").
func itemLink(raw rssItem) string {
	for _, link := range raw.Links {
		if strings.TrimSpace(link) != "" {
			return link
		}
	}
	if !strings.EqualFold(strings.TrimSpace(raw.GUID.IsPermaLink), "false") {
		return raw.GUID.Value
	}
	return ""
}

func firstAbsoluteLink(links []string) *url.URL {
	for _, link := range links {
		parsed, err := url.Parse(strings.TrimSpace(link))
		if err == nil && isWebURL(parsed) {
			return parsed
		}
	}
	return nil
}

func isWebURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// trackingParams are query keys that only say where a click came from. Stripping them
// keeps the reader's click from being attributed to us, and makes two links to the
// same article compare equal.
var trackingParams = map[string]bool{
	".tsrc":             true, // Yahoo's "came from RSS"
	"guccounter":        true, // Yahoo consent redirect state
	"guce_referrer":     true,
	"guce_referrer_sig": true,
	"ncid":              true,
	"cmpid":             true,
	"fbclid":            true,
	"gclid":             true,
	"mc_cid":            true,
	"mc_eid":            true,
	"soc_src":           true,
	"soc_trk":           true,
	"yptr":              true,
}

// cleanArticleURL resolves link against base, keeps it only if it is an absolute
// http(s) URL, and drops tracking parameters.
func cleanArticleURL(link string, base *url.URL) (string, bool) {
	link = strings.TrimSpace(html.UnescapeString(link))
	if link == "" {
		return "", false
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return "", false
	}
	if !parsed.IsAbs() {
		if base == nil {
			return "", false
		}
		parsed = base.ResolveReference(parsed)
	}
	if !isWebURL(parsed) {
		return "", false
	}
	if parsed.RawQuery != "" {
		query := parsed.Query()
		changed := false
		for key := range query {
			lower := strings.ToLower(key)
			// Google News appends oc=5 to every article link; it is its own click tag,
			// and the link resolves the same without it.
			googleTag := lower == "oc" && strings.EqualFold(parsed.Hostname(), "news.google.com")
			if trackingParams[lower] || strings.HasPrefix(lower, "utm_") || googleTag {
				query.Del(key)
				changed = true
			}
		}
		if changed {
			parsed.RawQuery = query.Encode()
		}
	}
	return parsed.String(), true
}

// offSiteSource names the publisher of an item that did not name one. An article on
// the feed's own site is the feed's publisher's; one on another site (Yahoo syndicates
// Stocktwits, for one) is that site's, by its host name rather than a borrowed label.
func offSiteSource(articleURL string, feedHome *url.URL, fallback string) string {
	article, err := url.Parse(articleURL)
	if err != nil || feedHome == nil {
		return fallback
	}
	articleSite, homeSite := siteOf(article.Hostname()), siteOf(feedHome.Hostname())
	if articleSite == "" || articleSite == homeSite {
		return fallback
	}
	return strings.TrimPrefix(strings.ToLower(article.Hostname()), "www.")
}

// siteOf is the last two labels of a host: enough to tell finance.yahoo.com and
// feeds.finance.yahoo.com apart from stocktwits.com.
func siteOf(host string) string {
	labels := strings.Split(strings.ToLower(strings.TrimSuffix(host, ".")), ".")
	if len(labels) < 2 {
		return strings.Join(labels, ".")
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

var markup = regexp.MustCompile(`<[^>]*>`)

// cleanText decodes entities the XML layer left behind (feeds double-encode: `&amp;#39;`),
// drops markup a CDATA section smuggled in, collapses whitespace, and bounds the length.
func cleanText(raw string, maxRunes int) string {
	text := html.UnescapeString(raw)
	text = markup.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

// cleanTitle is cleanText plus Google News' habit of appending " - Publisher" to every
// title, which the row already prints under it.
func cleanTitle(raw, source string) string {
	title := cleanText(raw, maxTitleRunes*2)
	if source != "" {
		for _, sep := range []string{" - ", " – ", " — ", " | "} {
			if trimmed, ok := strings.CutSuffix(title, sep+source); ok && strings.TrimSpace(trimmed) != "" {
				title = strings.TrimSpace(trimmed)
				break
			}
		}
	}
	return cleanText(title, maxTitleRunes)
}

// dateLayouts are the pubDate spellings seen in real feeds: RFC 822/1123 with a
// numeric zone or a name, a one-digit day, no weekday, and RFC 3339 (Yahoo's own
// top-stories feed, and dc:date).
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"02 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 MST",
	time.RFC822Z,
	time.RFC822,
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

// zoneOffsets are the US zone names feeds write out. Go parses an unknown zone name as
// UTC with that name attached, which would put an Eastern 4pm story at noon.
var zoneOffsets = map[string]string{
	"EST": "-0500", "EDT": "-0400",
	"CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600",
	"PST": "-0800", "PDT": "-0700",
	"GMT": "+0000", "UTC": "+0000", "UT": "+0000", "Z": "+0000",
}

// parseDate reads a feed timestamp into UTC.
func parseDate(raw string) (time.Time, bool) {
	value := strings.Join(strings.Fields(raw), " ")
	if value == "" {
		return time.Time{}, false
	}
	if i := strings.LastIndexByte(value, ' '); i > 0 {
		if offset, ok := zoneOffsets[strings.ToUpper(value[i+1:])]; ok {
			value = value[:i+1] + offset
		}
	}
	for _, layout := range dateLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// charsetReader lets a feed declare a legacy charset (ISO-8859-1, windows-1252) and
// still parse; encoding/xml only speaks UTF-8 on its own.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	encoding, err := htmlindex.Get(label)
	if err != nil {
		return nil, fmt.Errorf("unsupported charset %q", label)
	}
	return encoding.NewDecoder().Reader(input), nil
}
