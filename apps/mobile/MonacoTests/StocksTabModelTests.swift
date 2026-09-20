import Foundation
import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias MarketAssetDTO = Monaco.MarketAssetDTO
private typealias ListMarketAssetsResponse = Monaco.ListMarketAssetsResponse
private typealias PopularAssetsResponse = Monaco.PopularAssetsResponse
private typealias HeldAssetsResponse = Monaco.HeldAssetsResponse

@MainActor
private final class StubStocksDataSource: StocksTabDataSource {
    var searches: [(query: String, offset: Int)] = []
    var popularCalls = 0
    /// Per-query latency, so a slow page for an old query can land after a fast new one.
    var delays: [String: Duration] = [:]
    /// Per-offset latency, so page two can be held while page one answers at once.
    var offsetDelays: [Int: Duration] = [:]
    /// Per-offset failure, so page two can fail while the query's first page succeeds.
    var offsetErrors: [Int: Error] = [:]
    var errors: [String: Error] = [:]
    var popularError: Error?
    var heldCalls = 0
    var heldError: Error?
    var heldResponse = HeldAssetsResponse(held: [], upForVote: [])
    /// Rows served by `popular`, so a test can shape the mover strip.
    var popularAssets: [MarketAssetDTO] = [StubStocksDataSource.asset(symbol: "AAPLx")]
    var popularMarket: MarketStatusDTO?

    func search(query: String, offset: Int, limit: Int) async throws -> ListMarketAssetsResponse {
        searches.append((query, offset))
        if let delay = offsetDelays[offset] ?? delays[query] {
            try? await Task.sleep(for: delay)
        }
        if let error = offsetErrors[offset] ?? errors[query] { throw error }
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
        return PopularAssetsResponse(assets: popularAssets, market: popularMarket)
    }

    func held() async throws -> HeldAssetsResponse {
        heldCalls += 1
        if let heldError { throw heldError }
        return heldResponse
    }

    static func asset(
        symbol: String,
        change24h: String? = "0.012",
        spark: [Int64] = [180_000_000, 182_000_000, 185_000_000]
    ) -> MarketAssetDTO {
        MarketAssetDTO(
            symbol: symbol,
            name: "\(symbol) xStock",
            solanaMint: "Mint\(symbol)",
            routable: true,
            priceUsdcMicros: 185_000_000,
            change24h: change24h,
            sparkUsdcMicros: spark
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

    @Test func aFailedRefreshKeepsTheRowsOnScreen() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("tesla")
        try await settle()
        source.errors["tesla"] = Monaco.MonacoAPIError.httpStatus(500)
        await model.refreshSearch()

        #expect(model.searchState == .results)
        #expect(model.results.count == 2)
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

    /// The review finding: the stale-page guard compared the query *text*, which is an ABA check.
    /// Typing an L and taking it off again leaves the same text on screen under a different
    /// search, and page two of the first one passed the guard and appended itself into the list
    /// the second one had just cleared.
    @Test func aPageForARetypedQueryNeverLandsInTheListThatReplacedIt() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("aap")
        try await settle()
        #expect(model.results.map(\.symbol) == ["AAP-0", "AAP-1"])
        #expect(model.hasMore)

        // Page two of the "aap" on screen goes out, and is held in flight.
        source.offsetDelays[2] = .milliseconds(500)
        async let pageTwo: Void = model.loadMore()
        try await Task.sleep(for: .milliseconds(50))

        // The member types an L and deletes it: same text, a different search.
        model.updateQuery("aapl")
        model.updateQuery("aap")
        #expect(model.results.isEmpty)
        #expect(!model.isLoadingMore, "the new query's Load more must not be stuck on the old page")

        await pageTwo
        #expect(
            !model.results.contains { $0.symbol == "AAP-2" },
            "page two of the retyped query must not land in the list that replaced it"
        )
        try await settle()
        #expect(model.results.map(\.symbol) == ["AAP-0", "AAP-1"])
        #expect(model.hasMore)
    }

    /// The same orphaned page must not report *its* failure against the query that replaced it.
    @Test func aFailedPageForARetypedQueryDoesNotShowItsErrorOnTheNewOne() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("aap")
        try await settle()

        // Page two is held and then fails; the retyped query's own first page still answers.
        source.offsetDelays[2] = .milliseconds(400)
        source.offsetErrors[2] = Monaco.MonacoAPIError.httpStatus(500)
        async let pageTwo: Void = model.loadMore()
        try await Task.sleep(for: .milliseconds(50))

        model.updateQuery("aapl")
        model.updateQuery("aap")
        await pageTwo

        #expect(!model.loadMoreFailed, "the old page's failure belongs to a query nobody is reading")
        try await settle()
        #expect(model.searchState == .results)
    }

    /// `loadMoreFailed` drives the "Could not load more stocks." caption and nothing exercised it.
    @Test func aFailedLoadMoreKeepsTheRowsAndSaysSo() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("aap")
        try await settle()
        #expect(model.results.count == 2)

        source.errors["aap"] = Monaco.MonacoAPIError.httpStatus(500)
        await model.loadMore()

        #expect(model.loadMoreFailed)
        #expect(model.results.count == 2, "the rows already read stay on screen")
        #expect(model.searchState == .results)
        #expect(!model.isLoadingMore)

        // Trying again clears the caption.
        source.errors["aap"] = nil
        await model.loadMore()
        #expect(!model.loadMoreFailed)
        #expect(model.results.map(\.symbol) == ["AAP-0", "AAP-1", "AAP-2", "AAP-3"])
    }

    /// A failed pull-to-refresh kept the rows but retracted the spinner in silence, so stale
    /// prices read as fresh ones.
    @Test func aFailedRefreshKeepsTheRowsAndAdmitsItFailed() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        model.updateQuery("aap")
        try await settle()

        source.errors["aap"] = Monaco.MonacoAPIError.httpStatus(500)
        await model.refreshSearch()

        #expect(model.refreshFailed)
        #expect(model.results.map(\.symbol) == ["AAP-0", "AAP-1"])
        #expect(model.searchState == .results, "rows beat an error message")

        source.errors["aap"] = nil
        await model.refreshSearch()
        #expect(!model.refreshFailed)
    }

    // MARK: Sections

    @Test func rowsCarryTheirSparklineSoNoViewHasToBuildIt() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        await model.loadPopular()

        #expect(model.popularRows.map(\.id) == ["AAPLx"])
        #expect(model.popularRows.first?.spark != nil)
    }

    @Test func aSymbolWithNoDaySeriesStillMakesARow() async throws {
        let source = StubStocksDataSource()
        source.popularAssets = [StubStocksDataSource.asset(symbol: "NEWx", spark: [])]
        let model = StocksTabModel(dataSource: source)

        await model.loadPopular()

        #expect(model.popularRows.count == 1)
        #expect(model.popularRows.first?.spark == nil, "no series means no line, not a flat one")
    }

    @Test func topMoversAreThePopularRowsResortedByTheDaysMove() async throws {
        let source = StubStocksDataSource()
        source.popularAssets = [
            StubStocksDataSource.asset(symbol: "SMALLx", change24h: "0.004"),
            StubStocksDataSource.asset(symbol: "DROPx", change24h: "-0.081"),
            StubStocksDataSource.asset(symbol: "MIDx", change24h: "0.030"),
            StubStocksDataSource.asset(symbol: "QUIETx", change24h: nil),
        ]
        let model = StocksTabModel(dataSource: source)

        await model.loadPopular()

        #expect(model.moverRows.map(\.id) == ["DROPx", "MIDx", "SMALLx"])
        #expect(!model.moverRows.contains { $0.id == "QUIETx" }, "unknown is not a move")
    }

    @Test func theSessionOnTheEnvelopeReachesTheRows() async throws {
        let source = StubStocksDataSource()
        source.popularMarket = MarketSampleData.sessionAfterHours
        let model = StocksTabModel(dataSource: source)

        await model.loadPopular()

        #expect(model.afterHours)
    }

    @Test func yourCabalsAndOpenVotesArriveTogether() async throws {
        let source = StubStocksDataSource()
        source.heldResponse = MarketSampleData.heldAssetsResponse()
        let model = StocksTabModel(dataSource: source)

        await model.loadSocial()

        #expect(model.socialState == .loaded)
        #expect(model.heldRows.count == MarketSampleData.heldAssets.count)
        #expect(model.voteRows.count == MarketSampleData.votableAssets.count)
        #expect(model.heldRows.first?.subtitle == "2 cabals · your slice $294.70")
        #expect(model.voteRows.first?.subtitle == "1 open vote · Semis or bust")
    }

    @Test func cabalsThatOwnNothingIsAnAnswerNotAFailure() async throws {
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source)

        await model.loadSocial()

        #expect(model.socialState == .loaded)
        #expect(model.heldRows.isEmpty)
    }

    @Test func aFailedCabalReadIsItsOwnFailureAndLeavesTheCatalogueAlone() async throws {
        let source = StubStocksDataSource()
        source.heldError = Monaco.MonacoAPIError.httpStatus(500)
        let model = StocksTabModel(dataSource: source)

        await model.refreshEverything()

        #expect(model.socialState == .failed)
        #expect(model.popularState == .loaded, "the market is still live")
        #expect(!model.popularRows.isEmpty)
    }

    @Test func cabalRowsAlreadyOnScreenSurviveAFailedRefresh() async throws {
        let source = StubStocksDataSource()
        source.heldResponse = MarketSampleData.heldAssetsResponse()
        let model = StocksTabModel(dataSource: source)
        await model.loadSocial()

        source.heldError = Monaco.MonacoAPIError.httpStatus(500)
        await model.loadSocial()

        #expect(model.socialState == .loaded, "a stale holding beats an empty section")
        #expect(!model.heldRows.isEmpty)
    }

    @Test func anExpiredSessionFromTheCabalReadIsReported() async throws {
        let source = StubStocksDataSource()
        source.heldError = Monaco.MonacoAPIError.httpStatus(401)
        let model = StocksTabModel(dataSource: source)

        await model.loadSocial()

        #expect(model.sessionExpired)
        #expect(model.socialState == .loading, "a dead session is not an empty cabal list")
    }

    @Test func aRefreshWithinTheStaleWindowDoesNotReAskForTheCabals() async throws {
        let now = Date()
        let source = StubStocksDataSource()
        let model = StocksTabModel(dataSource: source, clock: { now })

        await model.refreshSocialIfStale()
        await model.refreshSocialIfStale()

        #expect(source.heldCalls == 1)
    }
}
