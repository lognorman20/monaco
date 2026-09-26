import Foundation

/// One headline, as `GET /v1/assets/{symbol}/news` and `GET /v1/news/market` send it.
public struct NewsItemDTO: Codable, Equatable, Sendable {
    public let title: String
    /// The article. The server only sends absolute http(s) links, with tracking
    /// parameters already removed.
    public let url: String
    /// The publisher: "Reuters", "Yahoo Finance", or an article's own host.
    public let source: String
    /// RFC 3339 UTC. Nil when the feed gave no usable date.
    public let publishedAt: String?

    public init(title: String, url: String, source: String, publishedAt: String?) {
        self.title = title
        self.url = url
        self.source = source
        self.publishedAt = publishedAt
    }
}

/// A list of headlines, newest first, and when the server read them from the feed.
public struct NewsFeedDTO: Codable, Equatable, Sendable {
    public let items: [NewsItemDTO]
    /// When the list was read upstream. A list the server kept through a feed outage
    /// keeps the time it was actually read.
    public let asOf: String

    public init(items: [NewsItemDTO], asOf: String) {
        self.items = items
        self.asOf = asOf
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = try container.decodeIfPresent([NewsItemDTO].self, forKey: .items) ?? []
        asOf = try container.decodeIfPresent(String.self, forKey: .asOf) ?? ""
    }
}

/// How old a headline is, in the words the rest of the app uses for time.
public enum NewsAge {
    /// "now", "12m", "3h", then a date: "Sep 24" (`RelativeTimeFormatter`'s words, so a
    /// headline and a chat message say the same thing about the same hour). Empty when
    /// the feed gave no date.
    public static func label(_ publishedAt: String?, now: Date = Date(), calendar: Calendar = .current) -> String {
        guard let publishedAt, !publishedAt.isEmpty else { return "" }
        return RelativeTimeFormatter.label(iso: publishedAt, now: now, calendar: calendar)
    }

    /// The same age for VoiceOver, which reads "3h" as "3 h": "3 hours ago",
    /// "September 24". Empty when the feed gave no date.
    public static func spoken(_ publishedAt: String?, now: Date = Date(), calendar: Calendar = .current) -> String {
        guard let publishedAt, let date = SharedFormatters.iso8601Date(from: publishedAt.trimmingCharacters(in: .whitespaces)) else {
            return ""
        }
        let elapsed = now.timeIntervalSince(date)
        if elapsed < 60 { return "just now" }
        if elapsed < 3600 {
            let minutes = Int(elapsed / 60)
            return minutes == 1 ? "1 minute ago" : "\(minutes) minutes ago"
        }
        if elapsed < 86_400 {
            let hours = Int(elapsed / 3600)
            return hours == 1 ? "1 hour ago" : "\(hours) hours ago"
        }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return SharedFormatters.string(
            from: date,
            pattern: .fixed(sameYear ? "MMMM d" : "MMMM d, yyyy"),
            locale: Locale(identifier: "en_US_POSIX"),
            calendar: calendar
        )
    }
}

/// One headline row, everything the view draws worked out once.
public struct NewsHeadline: Identifiable, Equatable, Sendable {
    public let title: String
    public let url: URL
    /// "Reuters · 3h", or just "Reuters" when the feed gave no date.
    public let stamp: String
    /// What VoiceOver reads: "Reuters, 3 hours ago. Wall Street ends higher".
    public let spoken: String

    public var id: String { url.absoluteString }

    /// The rows for a feed. An item with no title, or a link that is not an http(s)
    /// page, is left out: the reader can only open a web page, and a row that does
    /// nothing when tapped is worse than no row.
    public static func lines(_ items: [NewsItemDTO], now: Date = Date(), calendar: Calendar = .current) -> [NewsHeadline] {
        var seen = Set<String>()
        return items.compactMap { item in
            let title = item.title.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !title.isEmpty,
                  let url = URL(string: item.url.trimmingCharacters(in: .whitespacesAndNewlines)),
                  let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
                  url.host?.isEmpty == false,
                  seen.insert(url.absoluteString).inserted
            else { return nil }
            let source = item.source.trimmingCharacters(in: .whitespacesAndNewlines)
            let age = NewsAge.label(item.publishedAt, now: now, calendar: calendar)
            let spokenAge = NewsAge.spoken(item.publishedAt, now: now, calendar: calendar)
            let stamp = [source, age].filter { !$0.isEmpty }.joined(separator: " · ")
            let byline = [source, spokenAge].filter { !$0.isEmpty }.joined(separator: ", ")
            return NewsHeadline(
                title: title,
                url: url,
                stamp: stamp,
                spoken: byline.isEmpty ? title : "\(byline). \(title)"
            )
        }
    }
}

/// Every word the news rows and their sections say.
public enum NewsCopy {
    public static let sectionTitle = "News"
    public static let marketSectionTitle = "Market today"
    public static let seeAll = "See all"
    public static let failed = "Couldn't load the news."
    public static let retry = "Try again"
    public static let loading = "Loading headlines"
    public static let opensArticle = "Opens the article"

    /// "No news for Alphabet yet."
    public static func empty(_ companyName: String) -> String {
        let name = companyName.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? "No news yet." : "No news for \(name) yet."
    }

    /// The full list's title: "Alphabet news".
    public static func listTitle(_ companyName: String) -> String {
        let name = companyName.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty ? sectionTitle : "\(name) news"
    }

    /// How many headlines the stock screen shows before "See all".
    public static let previewCount = 3
}

/// How often a screen showing headlines re-reads them while it is open. The server
/// keeps each list for ten minutes, so anything faster would only read its cache.
public enum NewsRefresh {
    public static let interval: Duration = .seconds(300)
}
