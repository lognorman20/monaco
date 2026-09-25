import XCTest
@testable import MonacoCore

/// The news contract, the age words, and the rows built from them.
final class NewsDTOTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_790_378_100) // 2026-09-25T23:15:00Z

    private var utc: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar
    }

    // MARK: Decoding

    func testDecode_aFeedAsTheServerSendsIt() throws {
        // Arrange
        let json = """
        {"items":[
          {"title":"Wall Street ends higher as investors buy AI stocks; Microsoft rallies","url":"https://www.reuters.com/markets/us/","source":"Reuters","publishedAt":"2026-09-25T21:39:36Z"},
          {"title":"Alphabet vs. Apple","url":"https://finance.yahoo.com/b.html","source":"Yahoo Finance","publishedAt":null}
        ],"asOf":"2026-09-25T23:10:00Z"}
        """

        // Act
        let feed = try JSONDecoder().decode(NewsFeedDTO.self, from: Data(json.utf8))

        // Assert
        XCTAssertEqual(feed.items.count, 2)
        XCTAssertEqual(feed.asOf, "2026-09-25T23:10:00Z")
        XCTAssertEqual(feed.items[0].source, "Reuters")
        XCTAssertEqual(feed.items[0].publishedAt, "2026-09-25T21:39:36Z")
        XCTAssertNil(feed.items[1].publishedAt, "a null date is an undated headline, not a decode failure")
    }

    func testDecode_aMissingDateKeyIsUndated() throws {
        let json = #"{"items":[{"title":"T","url":"https://example.com/a","source":"S"}],"asOf":"2026-09-25T23:10:00Z"}"#

        let feed = try JSONDecoder().decode(NewsFeedDTO.self, from: Data(json.utf8))

        XCTAssertNil(feed.items.first?.publishedAt)
    }

    func testDecode_anEmptyFeed() throws {
        let feed = try JSONDecoder().decode(NewsFeedDTO.self, from: Data(#"{"items":[],"asOf":"2026-09-25T23:10:00Z"}"#.utf8))

        XCTAssertTrue(feed.items.isEmpty)
    }

    func testDecode_aFeedWithoutItemsIsEmptyNotAFailure() throws {
        let feed = try JSONDecoder().decode(NewsFeedDTO.self, from: Data(#"{"asOf":"2026-09-25T23:10:00Z"}"#.utf8))

        XCTAssertTrue(feed.items.isEmpty)
    }

    // MARK: Age

    func testAge_label() {
        let cases: [(String?, String)] = [
            ("2026-09-25T23:14:30Z", "now"),
            ("2026-09-25T23:03:00Z", "12m"),
            ("2026-09-25T20:15:00Z", "3h"),
            ("2026-09-24T09:00:00Z", "Sep 24"),
            ("2025-12-31T09:00:00Z", "Dec 31, 2025"),
            // A clock a little ahead of ours is still "now", not a negative age.
            ("2026-09-25T23:16:00Z", "now"),
            (nil, ""),
            ("", ""),
            ("yesterday", ""),
        ]
        for (published, expected) in cases {
            XCTAssertEqual(NewsAge.label(published, now: now, calendar: utc), expected, "publishedAt \(published ?? "nil")")
        }
    }

    func testAge_spoken() {
        let cases: [(String?, String)] = [
            ("2026-09-25T23:14:30Z", "just now"),
            ("2026-09-25T23:14:00Z", "1 minute ago"),
            ("2026-09-25T23:03:00Z", "12 minutes ago"),
            ("2026-09-25T22:15:00Z", "1 hour ago"),
            ("2026-09-25T20:15:00Z", "3 hours ago"),
            ("2026-09-24T09:00:00Z", "September 24"),
            ("2025-12-31T09:00:00Z", "December 31, 2025"),
            (nil, ""),
        ]
        for (published, expected) in cases {
            XCTAssertEqual(NewsAge.spoken(published, now: now, calendar: utc), expected, "publishedAt \(published ?? "nil")")
        }
    }

    // MARK: Rows

    func testLines_stampAndSpokenText() throws {
        // Arrange
        let items = [
            NewsItemDTO(title: "Wall Street ends higher", url: "https://www.reuters.com/markets/us/", source: "Reuters", publishedAt: "2026-09-25T20:15:00Z"),
            NewsItemDTO(title: "Undated story", url: "https://example.com/a", source: "Yahoo Finance", publishedAt: nil),
        ]

        // Act
        let lines = NewsHeadline.lines(items, now: now, calendar: utc)

        // Assert
        XCTAssertEqual(lines.count, 2)
        XCTAssertEqual(lines[0].stamp, "Reuters · 3h")
        XCTAssertEqual(lines[0].spoken, "Reuters, 3 hours ago. Wall Street ends higher")
        XCTAssertEqual(lines[0].url.absoluteString, "https://www.reuters.com/markets/us/")
        XCTAssertEqual(lines[1].stamp, "Yahoo Finance", "no date, no dangling separator")
    }

    func testLines_leaveOutRowsTheReaderCannotOpen() {
        // Arrange
        let items = [
            NewsItemDTO(title: "Fine", url: "https://example.com/a", source: "S", publishedAt: nil),
            NewsItemDTO(title: "A script", url: "javascript:alert(1)", source: "S", publishedAt: nil),
            NewsItemDTO(title: "A mail link", url: "mailto:desk@example.com", source: "S", publishedAt: nil),
            NewsItemDTO(title: "Not a URL", url: "", source: "S", publishedAt: nil),
            NewsItemDTO(title: "   ", url: "https://example.com/b", source: "S", publishedAt: nil),
            NewsItemDTO(title: "Same link again", url: "https://example.com/a", source: "S", publishedAt: nil),
        ]

        // Act
        let lines = NewsHeadline.lines(items, now: now, calendar: utc)

        // Assert
        XCTAssertEqual(lines.map(\.title), ["Fine"])
    }

    func testLines_sampleFeedsReadTheirAges() {
        let apple = NewsHeadline.lines(NewsSampleData.apple(now: now).items, now: now, calendar: utc)
        let market = NewsHeadline.lines(NewsSampleData.market(now: now).items, now: now, calendar: utc)

        XCTAssertEqual(apple.prefix(3).map(\.stamp), ["Yahoo Finance · 12m", "Yahoo Finance · 1h", "247wallst.com · 3h"])
        XCTAssertEqual(apple.last?.stamp, "Yahoo Finance · Sep 23")
        XCTAssertEqual(market.count, 4)
        XCTAssertTrue(NewsSampleData.empty(now: now).items.isEmpty)
    }

    // MARK: Copy

    func testCopy() {
        XCTAssertEqual(NewsCopy.empty("Alphabet"), "No news for Alphabet yet.")
        XCTAssertEqual(NewsCopy.empty("  "), "No news yet.")
        XCTAssertEqual(NewsCopy.listTitle("SpaceX"), "SpaceX news")
        XCTAssertEqual(NewsCopy.listTitle(""), "News")
    }

    func testCopy_usesNoForbiddenWords() {
        let strings = [
            NewsCopy.sectionTitle, NewsCopy.marketSectionTitle, NewsCopy.seeAll, NewsCopy.failed,
            NewsCopy.retry, NewsCopy.loading, NewsCopy.opensArticle,
            NewsCopy.empty("Apple"), NewsCopy.listTitle("Apple"),
        ]
        XCTAssertTrue(MainFlowCopyAudit.stringsAreClean(strings), "\(strings)")
    }
}

/// The two news routes over the wire.
final class NewsAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func makeClient() -> MonacoAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: URLSession(configuration: configuration),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private func respond(status: Int = 200, _ body: String, capturing capture: @escaping (URLRequest) -> Void = { _ in }) {
        MockURLProtocol.requestHandler = { request in
            capture(request)
            let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
            return (response, Data(body.utf8))
        }
    }

    func testGetAssetNews_asksForTheSymbolsHeadlines() async throws {
        // Arrange
        var captured: URLRequest?
        respond(#"{"items":[{"title":"T","url":"https://example.com/a","source":"Reuters","publishedAt":"2026-09-25T21:39:36Z"}],"asOf":"2026-09-25T23:10:00Z"}"#) {
            captured = $0
        }

        // Act
        let feed = try await makeClient().getAssetNews(symbol: "AAPLx")

        // Assert
        XCTAssertEqual(captured?.url?.absoluteString, "https://api.test/v1/assets/AAPLx/news", "no stray query marker")
        XCTAssertEqual(captured?.httpMethod, "GET")
        XCTAssertEqual(captured?.value(forHTTPHeaderField: "Authorization"), "Bearer \(TestFixtures.fixtureSessionToken)")
        XCTAssertEqual(feed.items.count, 1)
    }

    func testGetMarketNews_asksForTheMarketPulse() async throws {
        var captured: URLRequest?
        respond(#"{"items":[],"asOf":"2026-09-25T23:10:00Z"}"#) { captured = $0 }

        let feed = try await makeClient().getMarketNews()

        XCTAssertEqual(captured?.url?.absoluteString, "https://api.test/v1/news/market")
        XCTAssertTrue(feed.items.isEmpty)
    }

    func testGetAssetNews_anUnavailableFeedThrows() async {
        respond(status: 503, #"{"error":"news unavailable","requestId":"r1"}"#)

        do {
            _ = try await makeClient().getAssetNews(symbol: "AAPLx")
            XCTFail("a 503 should throw")
        } catch let error as MonacoAPIError {
            XCTAssertEqual(error.statusCode, 503)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }
}
