import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// A service that answers from queues, one result per call, so a test can script a load that
/// works and a refresh that does not.
@MainActor
final class ScriptedPortfolioService: PortfolioService {
    var portfolios: [Result<PortfolioDTO, Error>] = []
    var pages: [Result<HistoryPageDTO, Error>] = []
    private(set) var historyCalls: [(filter: HistoryFilter, cursor: String?)] = []

    func portfolio() async throws -> PortfolioDTO {
        try portfolios.removeFirst().get()
    }

    func history(filter: HistoryFilter, cursor: String?) async throws -> HistoryPageDTO {
        historyCalls.append((filter, cursor))
        return try pages.removeFirst().get()
    }

    func exportCSV(filter: HistoryFilter) async throws -> Data {
        Data("date,kind,status,cabal,stock,amount_usd,quantity,id\n".utf8)
    }
}

private func row(_ id: String, _ kind: String = "fund", status: String = "done", minutesAgo: Double = 0) -> HistoryItemDTO {
    HistoryItemDTO(id: id, kind: kind, status: status, groupName: "Sunday Investors", amountUsd: "10.00",
                   at: Date(timeIntervalSince1970: 1_790_000_000 - minutesAgo * 60))
}

private func page(_ ids: [String], cursor: String? = nil) -> HistoryPageDTO {
    HistoryPageDTO(items: ids.enumerated().map { row($0.element, minutesAgo: Double($0.offset)) }, nextCursor: cursor)
}

@MainActor
struct PortfolioModelTests {
    @Test func aFirstLoadShowsThePortfolio() async {
        let service = ScriptedPortfolioService()
        service.portfolios = [.success(PortfolioSampleData.portfolio)]
        let model = PortfolioModel(service: service)

        await model.load()

        #expect(model.state == .loaded(PortfolioSampleData.portfolio))
    }

    @Test func aFirstLoadThatFailsSaysWhyWithoutAStatusCode() async {
        let service = ScriptedPortfolioService()
        service.portfolios = [.failure(URLError(.notConnectedToInternet))]
        let model = PortfolioModel(service: service)

        await model.load()

        #expect(model.state == .failed("Check your connection and try again."))
    }

    /// A refresh that fails with the portfolio on screen keeps it there and asks for a toast.
    @Test func aFailedRefreshKeepsThePortfolio() async {
        let service = ScriptedPortfolioService()
        service.portfolios = [.failure(URLError(.timedOut))]
        let model = PortfolioModel(service: service, state: .loaded(PortfolioSampleData.portfolio))

        let quiet = await model.refresh()

        #expect(quiet == false)
        #expect(model.portfolio == PortfolioSampleData.portfolio)
    }

    @Test func aPollThatFailsThrowsForTheBackoffAndChangesNothing() async {
        let service = ScriptedPortfolioService()
        service.portfolios = [.failure(URLError(.timedOut))]
        let model = PortfolioModel(service: service, state: .loaded(PortfolioSampleData.portfolio))

        await #expect(throws: URLError.self) { try await model.poll() }
        #expect(model.portfolio == PortfolioSampleData.portfolio)
    }

    @Test func togglingAHoldingOpensAndClosesItsCabals() {
        let model = PortfolioModel(service: ScriptedPortfolioService())

        model.toggle("AAPLx")
        #expect(model.expanded == ["AAPLx"])
        model.toggle("AAPLx")
        #expect(model.expanded.isEmpty)
    }

    /// The sample the screenshots are shot from adds up the way the API promises.
    @Test func theSamplePortfolioAddsUp() {
        let portfolio = PortfolioSampleData.portfolio
        let holdings = portfolio.holdings.reduce(Decimal(0)) { $0 + PortfolioMath.decimal($1.valueUsd) }
        #expect(holdings + PortfolioMath.decimal(portfolio.cashUsd) == PortfolioMath.decimal(portfolio.totalUsd))
        for holding in portfolio.holdings {
            let cabals = holding.cabals.reduce(Decimal(0)) { $0 + PortfolioMath.decimal($1.valueUsd) }
            #expect(cabals == PortfolioMath.decimal(holding.valueUsd), "\(holding.symbol)")
        }
    }
}

@MainActor
struct HistoryModelTests {
    @Test func theFirstPageLoadsAndKeepsItsCursor() async {
        let service = ScriptedPortfolioService()
        service.pages = [.success(page(["a", "b"], cursor: "c1"))]
        let model = HistoryModel(service: service)

        await model.loadFirstPage()

        #expect(model.phase == .loaded)
        #expect(model.items.map(\.id) == ["a", "b"])
        #expect(model.nextCursor == "c1")
    }

    @Test func scrollingToTheLastRowLoadsTheNextPageOnce() async {
        let service = ScriptedPortfolioService()
        service.pages = [.success(page(["c", "b"], cursor: nil))]
        let model = HistoryModel(service: service, phase: .loaded, items: page(["a", "b"]).items, nextCursor: "c1")

        await model.loadMoreIfNeeded(currentItem: model.items[0])
        #expect(service.historyCalls.isEmpty, "only the last row asks for more")

        await model.loadMoreIfNeeded(currentItem: model.items[1])
        #expect(service.historyCalls.map(\.cursor) == ["c1"])
        #expect(model.items.map(\.id) == ["a", "b", "c"], "a row already on screen is not repeated")
        #expect(model.nextCursor == nil)
    }

    @Test func aFailedNextPageOffersARetryAndKeepsTheRows() async {
        let service = ScriptedPortfolioService()
        service.pages = [.failure(URLError(.timedOut))]
        let model = HistoryModel(service: service, phase: .loaded, items: page(["a"]).items, nextCursor: "c1")

        await model.loadMore()

        #expect(model.loadMoreFailed)
        #expect(model.items.map(\.id) == ["a"])
        #expect(model.nextCursor == "c1")
    }

    @Test func choosingAFilterStartsOverWithIt() async {
        let service = ScriptedPortfolioService()
        service.pages = [.success(page(["buy-1"]))]
        let model = HistoryModel(service: service, phase: .loaded, items: page(["a", "b"]).items, nextCursor: "c1")

        await model.select(.buy)

        #expect(service.historyCalls.map(\.filter) == [.buy])
        #expect(service.historyCalls.map(\.cursor) == [nil])
        #expect(model.items.map(\.id) == ["buy-1"])
        #expect(model.nextCursor == nil)
    }

    @Test func aFirstPageThatFailsIsTheFailureState() async {
        let service = ScriptedPortfolioService()
        service.pages = [.failure(URLError(.notConnectedToInternet))]
        let model = HistoryModel(service: service)

        let quiet = await model.loadFirstPage()

        #expect(quiet)
        #expect(model.phase == .failed("Check your connection and try again."))
    }

    @Test func thePollRefreshesTheTopAndKeepsWhatWasPagedIn() async {
        let service = ScriptedPortfolioService()
        let fresh = HistoryPageDTO(items: [row("new"), row("a", status: "done"), row("b")], nextCursor: "fresh")
        service.pages = [.success(fresh)]
        let model = HistoryModel(
            service: service, phase: .loaded,
            items: [row("a", status: "pending"), row("b"), row("c"), row("d")], nextCursor: "deep"
        )

        try? await model.poll()

        #expect(model.items.map(\.id) == ["new", "a", "b", "c", "d"])
        #expect(model.items[1].status == "done")
        #expect(model.nextCursor == "deep")
    }

    @Test func exportWritesTheFileTheShareSheetSends() async throws {
        let model = HistoryModel(service: ScriptedPortfolioService(), phase: .loaded, items: page(["a"]).items)

        let url = try await model.exportFile()

        #expect(url.lastPathComponent == "monaco-history.csv")
        #expect(try String(contentsOf: url, encoding: .utf8).hasPrefix("date,kind,status"))
        #expect(model.isExporting == false)
    }
}

struct HistoryMergeTests {
    @Test func whenTheFreshPageReachesTheLoadedRowsTheTailStays() {
        let merged = HistoryMerge.merge(fresh: page(["x", "a"], cursor: "f"), existing: page(["a", "b", "c"]).items, existingCursor: "deep")

        #expect(merged.items.map(\.id) == ["x", "a", "b", "c"])
        #expect(merged.nextCursor == "deep")
    }

    @Test func whenMoreArrivedThanAPageHoldsTheListRestarts() {
        let merged = HistoryMerge.merge(fresh: page(["x", "y"], cursor: "f"), existing: page(["a", "b"]).items, existingCursor: "deep")

        #expect(merged.items.map(\.id) == ["x", "y"])
        #expect(merged.nextCursor == "f")
    }

    @Test func aHistoryThatFitsOnOnePageIsJustThePage() {
        let merged = HistoryMerge.merge(fresh: page(["x", "a"]), existing: page(["a", "b"]).items, existingCursor: "deep")

        #expect(merged.items.map(\.id) == ["x", "a"])
        #expect(merged.nextCursor == nil)
    }
}

struct PortfolioReceiptTests {
    @Test func aTradeOpensItsSwapReceipt() {
        let item = HistoryItemDTO(id: "row", kind: "bot_sell", status: "done", groupName: "Semis", symbol: "NVDAx",
                                  name: "NVIDIA", amountUsd: "156.00", quantity: "0.6", at: Date(), transactionId: "tx-9")

        let activity = HistoryReceiptItem.activityItem(for: item, receipt: .transaction("tx-9"))

        #expect(activity.id == "tx-9")
        #expect(activity.kind == "sell")
        #expect(activity.status == "confirmed")
        #expect(activity.initiatedBy == "agent")
        #expect(activity.proceedsUsdcMicros == "156000000")
    }

    @Test func aFundOpensItsDepositReceipt() {
        let item = HistoryItemDTO(id: "dep-1", kind: "fund", status: "pending", groupName: "Semis", amountUsd: "50.00", at: Date())

        let activity = HistoryReceiptItem.activityItem(for: item, receipt: .deposit("dep-1"))

        #expect(activity.id == "dep-1")
        #expect(activity.kind == "deposit")
        #expect(activity.status == "pending")
        #expect(activity.amountMicros == 50_000_000)
    }
}

/// The backend sends each cabal's tint by the same rule the app uses; these are the ids
/// `TestCabalTintName_matchesTheAppsRule` pins in Go, so the two copies cannot drift.
struct PortfolioTintParityTests {
    @Test(arguments: [
        ("g1", .moss),
        ("g2", .ochre),
        ("g3", .plum),
        ("g5", .pine),
        ("3F2504E0-4F89-11D3-9A0C-0305E82C3301", .ochre),
    ] as [(String, MonacoTheme.CabalTint)])
    func tintMatchesTheBackend(groupId: String, tint: MonacoTheme.CabalTint) {
        #expect(MonacoTheme.CabalTint.forGroupId(groupId) == tint)
    }
}
