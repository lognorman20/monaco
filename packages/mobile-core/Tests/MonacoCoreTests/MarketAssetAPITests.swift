import XCTest
@testable import MonacoCore

/// The asset routes over the wire: what the client asks for, and what it makes of
/// the answer.
final class MarketAssetAPITests: XCTestCase {
    override func tearDown() {
        MockURLProtocol.requestHandler = nil
        super.tearDown()
    }

    private func makeMockURLSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [MockURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func makeClient() -> MonacoAPIClient {
        MonacoAPIClient(
            baseURL: URL(string: "https://api.test")!,
            session: makeMockURLSession(),
            accessTokenProvider: { TestFixtures.fixtureSessionToken }
        )
    }

    private func respond(_ body: String, capturing capture: @escaping (URLRequest) -> Void = { _ in }) {
        MockURLProtocol.requestHandler = { request in
            capture(request)
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: nil,
                headerFields: ["Content-Type": "application/json"]
            )!
            return (response, Data(body.utf8))
        }
    }

    func testGetMarketAssetChart_sendsEveryRangeTheBackendSupports() async throws {
        for range in AssetChartRange.allCases {
            var capturedQuery: String?
            respond(#"{"points":[],"emptyReason":"price history unavailable","range":"\#(range.rawValue)"}"#) { request in
                capturedQuery = request.url?.query
            }

            let chart = try await makeClient().getMarketAssetChart(symbol: "AAPLx", range: range)
            XCTAssertEqual(capturedQuery, "range=\(range.rawValue)")
            XCTAssertEqual(chart.range, range, "the response should name the range it was built for")
        }
    }

    func testGetMarketAssetChart_decodesADenseCandleSeries() async throws {
        respond("""
        {
          "points":[
            {"timestamp":1790064000,"priceUsdcMicros":229400000,"openUsdcMicros":229000000,"highUsdcMicros":229500000,"lowUsdcMicros":228200000},
            {"timestamp":1790064300,"priceUsdcMicros":231400000,"openUsdcMicros":231000000,"highUsdcMicros":231800000,"lowUsdcMicros":230400000}
          ],
          "previousCloseUsdcMicros":226500000,
          "range":"1D",
          "source":"benchmarks",
          "market":{"session":"open","isOpen":true,"afterHours":false,"nextSession":"after_hours","nextTransition":"2026-09-22T20:00:00Z","asOf":"2026-09-22T14:00:00Z"}
        }
        """)

        let chart = try await makeClient().getMarketAssetChart(symbol: "AAPLx", range: .oneDay)
        XCTAssertEqual(chart.points.count, 2)
        XCTAssertEqual(chart.previousCloseUsdcMicros, 226_500_000)
        XCTAssertEqual(chart.source, .benchmarks)
        XCTAssertTrue(chart.points.allSatisfy(\.hasCandle))
        XCTAssertEqual(chart.market?.nextSession, .afterHours)
    }

    func testGetMarketAssetChart_emptySeriesKeepsItsReason() async throws {
        respond(#"{"points":[],"emptyReason":"price history unavailable","range":"1Y","source":"benchmarks"}"#)

        let chart = try await makeClient().getMarketAssetChart(symbol: "NEWx", range: .oneYear)
        XCTAssertTrue(chart.points.isEmpty)
        XCTAssertEqual(chart.emptyReason, "price history unavailable")
        XCTAssertNil(chart.previousCloseUsdcMicros)
    }

    func testGetMarketAsset_decodesTheStatsGridAndTheComparison() async throws {
        var capturedPath: String?
        respond("""
        {
          "symbol":"AAPLx","name":"Apple","solanaMint":"Xsb","routable":true,
          "priceUsdcMicros":232050000,"change24h":"0.024500",
          "liquidity":{"label":"Via Jupiter","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":12},
          "marketSession":"after_hours","afterHours":true,
          "market":{"session":"after_hours","isOpen":false,"afterHours":true,"nextSession":"closed","asOf":"2026-09-22T21:00:00Z"},
          "stats":{"openUsdcMicros":229000000,"highUsdcMicros":231800000,"lowUsdcMicros":228200000,"previousCloseUsdcMicros":226500000,"week52HighUsdcMicros":262000000,"week52LowUsdcMicros":163000000,"spreadBps":12,"confUsdcMicros":30000},
          "stockVsToken":{
            "equity":{"source":"pyth_equity","status":"stale","priceUsdcMicros":231400000,"confUsdcMicros":30000,"publishedAt":"2026-09-22T20:00:00Z"},
            "token":{"source":"pyth_crypto","status":"live","priceUsdcMicros":234800000,"confUsdcMicros":110000,"publishedAt":"2026-09-22T20:59:57Z"},
            "premiumBps":147,
            "asOf":"2026-09-22T21:00:00Z"
          }
        }
        """) { request in
            capturedPath = request.url?.path
        }

        let detail = try await makeClient().getMarketAsset(symbol: "AAPLx")
        XCTAssertEqual(capturedPath, "/v1/assets/AAPLx")
        XCTAssertEqual(detail.marketSession, .afterHours)
        XCTAssertTrue(detail.afterHours)
        XCTAssertEqual(detail.stats?.week52LowUsdcMicros, 163_000_000)
        XCTAssertEqual(detail.stockVsToken?.equity.status, .stale)
        XCTAssertEqual(detail.stockVsToken?.premiumBps, 147)
    }

    func testGetMarketAsset_missingComparisonIsNotAnError() async throws {
        // The card is dropped when neither feed had a price. The rest of the screen
        // still has to load.
        respond("""
        {
          "symbol":"NEWx","name":"Newly listed","solanaMint":"NEW","routable":true,
          "priceUsdcMicros":41250000,
          "liquidity":{"label":"Via Jupiter","routable":true,"buyProbeUsdcMicros":1000000,"spreadBps":48},
          "marketSession":"open","afterHours":false
        }
        """)

        let detail = try await makeClient().getMarketAsset(symbol: "NEWx")
        XCTAssertNil(detail.stockVsToken)
        XCTAssertNil(detail.stats)
        XCTAssertEqual(detail.priceUsdcMicros, 41_250_000)
    }

    func testListMarketAssets_decodesTheSessionEnvelope() async throws {
        respond("""
        {
          "assets":[{"symbol":"AAPLx","name":"Apple","solanaMint":"Xsb","routable":true,"priceUsdcMicros":232050000,"change24h":"0.0245"}],
          "hasMore":false,
          "market":{"session":"pre_market","isOpen":false,"afterHours":true,"nextSession":"open","nextTransition":"2026-09-22T13:30:00Z","asOf":"2026-09-22T12:00:00Z"}
        }
        """)

        let page = try await makeClient().listMarketAssets(query: "AAPL")
        XCTAssertEqual(page.assets.count, 1)
        XCTAssertEqual(page.market?.session, .preMarket)
        XCTAssertTrue(page.market?.afterHours ?? false)
        XCTAssertEqual(page.market?.nextSession, .open)
    }

    func testGetPopularAssets_decodesTheSessionEnvelope() async throws {
        respond("""
        {"assets":[],"market":{"session":"closed","isOpen":false,"afterHours":true,"asOf":"2026-11-26T16:00:00Z","holiday":"Thanksgiving Day"}}
        """)

        let popular = try await makeClient().getPopularAssets()
        XCTAssertEqual(popular.market?.holiday, "Thanksgiving Day")
    }

    func testGetMarketAsset_malformedBodyStillThrows() async {
        respond(#"{"symbol":"AAPLx"}"#) // no name, mint or liquidity

        do {
            _ = try await makeClient().getMarketAsset(symbol: "AAPLx")
            XCTFail("expected a decoding error rather than a half-built detail")
        } catch {
            XCTAssertTrue(error is DecodingError, "got \(error)")
        }
    }
}
