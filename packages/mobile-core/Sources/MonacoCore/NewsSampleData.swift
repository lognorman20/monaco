import Foundation

/// Canned headlines for the sample harnesses: real headlines from the Yahoo Finance and
/// Google News feeds the server reads, captured on 2026-09-25, re-dated relative to
/// `now` so every screenshot shows the same ages ("12m", "3h", a date).
public enum NewsSampleData {
    /// Apple's feed: the stock the other stock-screen samples draw.
    public static func apple(now: Date = Date()) -> NewsFeedDTO {
        feed(now: now, [
            (12 * minute, "Yahoo Finance", "Evercore ISI’s Bullish iPhone Survey Faces a Reality Check From Pre-Order Data",
             "https://finance.yahoo.com/markets/stocks/articles/evercore-isi-bullish-iphone-survey-205754196.html"),
            (hour + 5 * minute, "Yahoo Finance", "Apple (AAPL) Reaches $250 Million Siri Settlement Over Recent iPhone Claims",
             "https://finance.yahoo.com/markets/stocks/articles/apple-aapl-reaches-250-million-190827546.html"),
            (3 * hour, "247wallst.com", "Apple Just Gained 10% in a Month. What Will It Take for AAPL Stock to Finally Hit $350?",
             "https://247wallst.com/investing/2026/09/25/apple-just-gained-10-in-a-month-what-will-it-take-for-aapl-stock-to-finally-hit-350/"),
            (4 * hour, "Yahoo Finance", "Qualcomm Stocks Jump as Apple Extends the Royalty Bridge",
             "https://finance.yahoo.com/markets/stocks/articles/qualcomm-stocks-jump-apple-extends-175240580.html"),
            (6 * hour, "Yahoo Finance", "Apple (AAPL) Rises Higher Than Market: Key Facts",
             "https://finance.yahoo.com/markets/stocks/articles/apple-aapl-rises-higher-market-204501112.html"),
            (9 * hour, "thestreet.com", "A forgotten tech giant makes a quiet comeback",
             "https://www.thestreet.com/technology/a-forgotten-tech-giant-is-quietly-making-a-comeback"),
            (26 * hour, "Yahoo Finance", "Nvidia, AMD Love This High-Tech Facilitator. So Does Wall Street.",
             "https://finance.yahoo.com/m/b8eae4aa-f7ed-33bb-9966-deffb558b2ed/nvidia%2C-amd-love-this.html"),
            (50 * hour, "Yahoo Finance", "Trump-Xi Meeting Puts These 5 Chinese AI Stocks in Focus",
             "https://finance.yahoo.com/technology/ai/articles/trump-xi-meeting-puts-5-183300560.html"),
        ])
    }

    /// The Stocks tab's market pulse.
    public static func market(now: Date = Date()) -> NewsFeedDTO {
        feed(now: now, [
            (18 * minute, "Reuters", "Wall Street ends higher as investors buy AI stocks; Microsoft rallies",
             "https://www.reuters.com/markets/us/"),
            (44 * minute, "Yahoo Finance", "Stock Market Today, Sept. 25: Microsoft Stock Jumps 4% After Revamping Copilot With Code Generation and Agentic AI Tools",
             "https://finance.yahoo.com/markets/stocks/"),
            (2 * hour, "WSJ", "Stocks Rise to Cap Week of Increasing Yields, Volatile Oil Prices",
             "https://www.wsj.com/finance/stocks"),
            (3 * hour, "CNBC", "'Fast Money' traders talk the stock market staying the course despite rising bond rates",
             "https://www.cnbc.com/fast-money/"),
        ])
    }

    /// A feed that answered with nothing.
    public static func empty(now: Date = Date()) -> NewsFeedDTO {
        NewsFeedDTO(items: [], asOf: iso(now))
    }

    private static let minute: TimeInterval = 60
    private static let hour: TimeInterval = 3600

    private static func feed(now: Date, _ rows: [(TimeInterval, String, String, String)]) -> NewsFeedDTO {
        NewsFeedDTO(
            items: rows.map { age, source, title, url in
                NewsItemDTO(title: title, url: url, source: source, publishedAt: iso(now.addingTimeInterval(-age)))
            },
            asOf: iso(now)
        )
    }

    private static func iso(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.string(from: date)
    }
}
