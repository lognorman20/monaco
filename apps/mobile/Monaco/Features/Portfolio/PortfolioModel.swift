import Foundation
import MonacoCore
import Observation

/// The portfolio screen's one value: loading, the portfolio, or why it failed. Data wins over
/// an error, like Home: once a portfolio is on screen a failed refresh is a toast, not a
/// replacement.
@Observable
@MainActor
final class PortfolioModel {
    enum State: Equatable {
        case loading
        case loaded(PortfolioDTO)
        case failed(String)
    }

    private(set) var state: State
    private(set) var isRetrying = false
    /// Holdings opened to show their cabals, by symbol.
    private(set) var expanded: Set<String> = []

    private let service: PortfolioService

    init(service: PortfolioService, state: State = .loading, expanded: Set<String> = []) {
        self.service = service
        self.state = state
        self.expanded = expanded
    }

    var portfolio: PortfolioDTO? {
        if case .loaded(let portfolio) = state { return portfolio }
        return nil
    }

    /// First load and Try again. A failure replaces the screen only when there is nothing on it.
    func load() async {
        guard !isRetrying else { return }
        isRetrying = true
        defer { isRetrying = false }
        if portfolio == nil { state = .loading }
        do {
            let fresh = try await service.portfolio()
            apply(fresh)
        } catch {
            guard !Self.isCancellation(error) else { return }
            if portfolio == nil { state = .failed(PortfolioFailureCopy.message(for: error)) }
        }
    }

    /// Pull to refresh. Returns false when it failed with the portfolio still on screen, so the
    /// screen can say so in a toast.
    func refresh() async -> Bool {
        do {
            apply(try await service.portfolio())
            return true
        } catch {
            if Self.isCancellation(error) { return true }
            if portfolio == nil { state = .failed(PortfolioFailureCopy.message(for: error)) }
            return portfolio == nil
        }
    }

    /// A background tick: quiet, and it throws so the poll loop backs off.
    func poll() async throws {
        apply(try await service.portfolio())
    }

    func toggle(_ symbol: String) {
        if expanded.contains(symbol) {
            expanded.remove(symbol)
        } else {
            expanded.insert(symbol)
        }
    }

    private func apply(_ fresh: PortfolioDTO) {
        QuietUpdate.apply(State.loaded(fresh), over: state) { state = $0 }
    }

    static func isCancellation(_ error: Error) -> Bool {
        error is CancellationError || error.isRequestCancellation
    }
}

/// The history screen: one filter, the rows loaded so far, and the cursor for the next page.
@Observable
@MainActor
final class HistoryModel {
    enum Phase: Equatable {
        case loading
        case loaded
        case failed(String)
    }

    private(set) var filter: HistoryFilter
    private(set) var phase: Phase
    private(set) var items: [HistoryItemDTO]
    private(set) var nextCursor: String?
    private(set) var isLoadingMore = false
    private(set) var loadMoreFailed = false
    private(set) var isExporting = false

    private let service: PortfolioService
    /// Bumped whenever the list is reset, so an answer for a filter the member has already left
    /// cannot land on the new one.
    private var generation = 0

    init(
        service: PortfolioService,
        filter: HistoryFilter = .all,
        phase: Phase = .loading,
        items: [HistoryItemDTO] = [],
        nextCursor: String? = nil
    ) {
        self.service = service
        self.filter = filter
        self.phase = phase
        self.items = items
        self.nextCursor = nextCursor
    }

    var sections: [HistoryDaySection] { HistoryDayGrouping.sections(items) }

    var hasMore: Bool { nextCursor != nil }

    func select(_ filter: HistoryFilter) async {
        guard filter != self.filter else { return }
        self.filter = filter
        items = []
        nextCursor = nil
        loadMoreFailed = false
        phase = .loading
        await loadFirstPage()
    }

    /// First page, loudly: the skeleton while it loads, the failure state if it cannot. Returns
    /// false when it failed with rows still on screen, for the screen to say so in a toast.
    @discardableResult
    func loadFirstPage() async -> Bool {
        generation += 1
        let mine = generation
        if items.isEmpty { phase = .loading }
        do {
            let page = try await service.history(filter: filter, cursor: nil)
            guard mine == generation else { return true }
            items = page.items
            nextCursor = page.nextCursor
            loadMoreFailed = false
            phase = .loaded
            return true
        } catch {
            guard mine == generation, !PortfolioModel.isCancellation(error) else { return true }
            if items.isEmpty {
                phase = .failed(PortfolioFailureCopy.message(for: error))
                return true
            }
            return false
        }
    }

    /// Infinite scroll: the next page once the last loaded row comes on screen.
    func loadMoreIfNeeded(currentItem: HistoryItemDTO) async {
        guard currentItem.id == items.last?.id, !loadMoreFailed else { return }
        await loadMore()
    }

    func loadMore() async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        let mine = generation
        isLoadingMore = true
        loadMoreFailed = false
        defer { isLoadingMore = false }
        do {
            let page = try await service.history(filter: filter, cursor: cursor)
            guard mine == generation else { return }
            let known = Set(items.map(\.id))
            items.append(contentsOf: page.items.filter { !known.contains($0.id) })
            nextCursor = page.nextCursor
        } catch {
            guard mine == generation, !PortfolioModel.isCancellation(error) else { return }
            loadMoreFailed = true
        }
    }

    /// A background tick re-reads the first page, so a pending row turns done while the member
    /// watches. Rows already paged in below it stay.
    func poll() async throws {
        let mine = generation
        let page = try await service.history(filter: filter, cursor: nil)
        guard mine == generation else { return }
        let merged = HistoryMerge.merge(fresh: page, existing: items, existingCursor: nextCursor)
        QuietUpdate.apply(merged.items, over: items) { items = $0 }
        nextCursor = merged.nextCursor
        if phase != .loaded { phase = .loaded }
    }

    /// Fetches the CSV for the current filter and writes it where the share sheet can pick it up.
    func exportFile() async throws -> URL {
        isExporting = true
        defer { isExporting = false }
        let data = try await service.exportCSV(filter: filter)
        let url = FileManager.default.temporaryDirectory.appending(path: "monaco-history.csv")
        try data.write(to: url, options: [.atomic, .completeFileProtection])
        return url
    }
}

/// How a re-read first page folds into the rows already on screen.
enum HistoryMerge {
    struct Result: Equatable {
        let items: [HistoryItemDTO]
        let nextCursor: String?
    }

    /// The fresh page replaces the top of the list. When its last row is one already loaded, the
    /// rows below that stay, with the cursor that continues them. When it is not — more rows
    /// arrived than a page holds — the list restarts from the fresh page and its own cursor,
    /// rather than guessing where the two meet.
    static func merge(fresh: HistoryPageDTO, existing: [HistoryItemDTO], existingCursor: String?) -> Result {
        guard let last = fresh.items.last,
              fresh.nextCursor != nil,
              let index = existing.firstIndex(where: { $0.id == last.id })
        else {
            return Result(items: fresh.items, nextCursor: fresh.nextCursor)
        }
        let freshIDs = Set(fresh.items.map(\.id))
        let tail = existing[(index + 1)...].filter { !freshIDs.contains($0.id) }
        return Result(items: fresh.items + tail, nextCursor: existingCursor)
    }
}
