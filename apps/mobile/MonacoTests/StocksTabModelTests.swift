import Foundation
import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias MarketAssetDTO = Monaco.MarketAssetDTO
private typealias ListMarketAssetsResponse = Monaco.ListMarketAssetsResponse
private typealias PopularAssetsResponse = Monaco.PopularAssetsResponse

@MainActor
private final class StubStocksDataSource: StocksTabDataSource {
    var searches: [(query: String, offset: Int)] = []
    var popularCalls = 0
    /// Per-query latency, so a slow page for an old query can land after a fast new one.
    var delays: [String: Duration] = [:]
    var errors: [String: Error] = [:]
    var popularError: Error?

    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        searches.append((query, offset))
        if let delay = delays[query] {
            try? await Task.sleep(for: delay)
        }
        if let error = errors[query] { throw error }
        if query == "none" {
            return ListMarketAssetsResponse(assets: [], hasMore: false)
        }
        let assets = (0..<2).map { index in
            Self.asset(symbol: "\(query.uppercased())-\(offset + index)")
        }
        return ListMarketAssetsResponse(assets: assets, hasMore: true)
    }

    func popular(limit: Int) async throws -> PopularAssetsResponse {
        popularCalls += 1
        if let popularError { throw popularError }
        return PopularAssetsResponse(assets: [Self.asset(symbol: "AAPLx")])
    }

    static func asset(symbol: String) -> MarketAssetDTO {
        MarketAssetDTO(
            symbol: symbol,
            name: "\(symbol) xStock",
            solanaMint: "Mint\(symbol)",
            routable: true,
            priceUsdcMicros: 185_000_000,
            change24h: "0.012"
        )
    }
}

@MainActor
struct StocksTabModelTests {
    private func settle() async throws {
        try await Task.sleep(for: .milliseconds(450))
    }

    @Test func typingQuicklySendsOnlyTheLastQuery() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("te")
        model.updateQuery("tes")
        model.updateQuery("tesla")
        try await settle()

        #expect(source.searches.map(\.query) == ["tesla"])
        #expect(model.searchState == .results)
    }

    /// The bug: page two of "a" was appended to the list the user was reading for "tesla",
    /// taking the offset and the has-more flag with it.
    @Test func aLoadMorePageForAnOldQueryNeverLandsInTheNewList() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("a")
        try await settle()
        #expect(model.results.count == 2)

        // Page two of "a" comes back long after "tesla" has replaced it on screen.
        source.delays["a"] = .milliseconds(700)
        let loadMore = Task { await model.loadMore() }
        try await Task.sleep(for: .milliseconds(50))
        model.updateQuery("tesla")
        try await settle()
        await loadMore.value

        #expect(model.trimmedQuery == "tesla")
        #expect(model.results.map(\.symbol) == ["TESLA-0", "TESLA-1"])
        #expect(model.searchState == .results)
    }

    /// The bug: the shared `defer` from a stale request cleared the loading flag mid-debounce,
    /// flashing "No matches for that search" over the query the user was still typing.
    @Test func aStalePageNeverFlashesAnEmptyOrFailedState() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)
        source.delays["a"] = .milliseconds(700)
        source.errors["a"] = Monaco.MonacoAPIError.httpStatus(500)

        model.updateQuery("a")
        try await Task.sleep(for: .milliseconds(350))
        model.updateQuery("tesla")
        try await Task.sleep(for: .milliseconds(900))

        #expect(model.searchState == .results)
        #expect(model.results.map(\.symbol) == ["TESLA-0", "TESLA-1"])
    }

    @Test func loadMoreAppendsWithoutDuplicatingRows() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("tesla")
        try await settle()
        await model.loadMore()

        #expect(model.results.count == 4)
        #expect(Set(model.results.map(\.symbol)).count == 4)
        #expect(source.searches.map(\.offset) == [0, 2])
    }

    @Test func noMatchesShowsEmptyState() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("none")
        try await settle()

        #expect(model.searchState == .empty)
        #expect(model.results.isEmpty)
    }

    @Test func serverFailureShowsRetryableError() async throws {
        let source = StubStocksDataSource()
        source.errors["tesla"] = Monaco.MonacoAPIError.httpStatus(500)
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("tesla")
        try await settle()

        #expect(model.searchState == .failed)
        #expect(!model.sessionExpired)
    }

    @Test func rejectedSessionAsksTheViewToSignOut() async throws {
        let source = StubStocksDataSource()
        source.errors["tesla"] = Monaco.MonacoAPIError.httpStatus(401)
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("tesla")
        try await settle()

        #expect(model.sessionExpired)
    }

    /// The bug: a missing token returned before the loading flag was cleared, so the tab
    /// showed "Loading stocks…" for ever.
    @Test func aMissingTokenEndsInAFailedState() async throws {
        let source = StubStocksDataSource()
        source.errors["tesla"] = Monaco.MonacoAPIError.missingAccessToken
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("tesla")
        try await settle()

        #expect(model.searchState == .failed)
    }

    @Test func popularFailureIsRetryableInsteadOfLookingEmpty() async throws {
        let source = StubStocksDataSource()
        source.popularError = Monaco.MonacoAPIError.httpStatus(500)
        let model = StocksTabModel(dataSource: source)

        await model.loadPopular()

        #expect(model.popularState == .failed)
        #expect(model.popular.isEmpty)
    }

    @Test func popularRefreshesOnlyOnceItIsStale() async throws {
        let source = StubStocksDataSource()
        var now = Date(timeIntervalSince1970: 1_000)
        let model = StocksTabModel(dataSource: source, clock: { now })

        await model.refreshPopularIfStale()
        await model.refreshPopularIfStale()
        #expect(source.popularCalls == 1)

        now = now.addingTimeInterval(StocksTabModel.popularStaleAfter)
        await model.refreshPopularIfStale()
        #expect(source.popularCalls == 2)
        #expect(model.popularState == .loaded)
    }

    /// The session's cached strip paints at once, and the prices are still refetched.
    @Test func seededPopularStillRefreshes() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.seedPopular([StubStocksDataSource.asset(symbol: "NVDAx")])
        #expect(model.popularState == .loaded)
        #expect(model.popular.map(\.symbol) == ["NVDAx"])

        await model.refreshPopularIfStale()
        #expect(source.popularCalls == 1)
        #expect(model.popular.map(\.symbol) == ["AAPLx"])
    }
}
