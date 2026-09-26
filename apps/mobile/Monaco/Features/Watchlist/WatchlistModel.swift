import MonacoCore
import Observation
import SwiftUI

/// The Stocks tab's watchlist section: the member's followed stocks as market rows, in their
/// order, plus the remove and reorder that change it.
///
/// Rows on screen survive a failed refresh — they are marked stale instead — and a write
/// that fails puts the list back the way it was and says so in a toast.
@Observable
@MainActor
final class WatchlistModel {
    enum LoadState: Equatable {
        /// Nothing has answered yet.
        case loading
        case loaded
        /// The first read failed; there are no rows to fall back on.
        case failed
    }

    /// Past this age a revisit to the tab refetches, the same as the tab's other rows.
    static let staleAfter: TimeInterval = StocksTabModel.popularStaleAfter
    /// The most skeleton rows the section holds room for while it loads.
    static let maximumSkeletonRows = 4

    private(set) var rows: [MarketRowData] = []
    private(set) var state: LoadState = .loading
    /// A refresh failed while rows were on screen. They stay, captioned as possibly stale.
    private(set) var refreshFailed = false
    private(set) var afterHours = false
    /// A confirmation or a failure to show on the tab.
    var toast: MonacoToast?
    /// The edit sheet is up.
    var isEditing = false

    private let dataSource: WatchlistDataSource
    private let memory: WatchlistMemory
    private let clock: () -> Date
    private var loadedAt: Date?
    private var isLoading = false

    init(dataSource: WatchlistDataSource, memory: WatchlistMemory, clock: @escaping () -> Date = Date.init) {
        self.dataSource = dataSource
        self.memory = memory
        self.clock = clock
    }

    var symbols: [String] { rows.map(\.asset.symbol) }

    /// How many skeleton rows to draw while the first read is in flight: as many as the
    /// watchlist had last time, so the tab does not jump when it lands. Zero for a member who
    /// has never watched anything — a section of placeholders for a feature they do not use
    /// would be noise.
    var skeletonRowCount: Int {
        guard state == .loading, rows.isEmpty else { return 0 }
        return min(memory.lastCount, Self.maximumSkeletonRows)
    }

    /// The one-line nudge under the search field, until the member first follows a stock.
    var showsFirstUseHint: Bool {
        state == .loaded && rows.isEmpty && !memory.hasWatched
    }

    /// The section's failure state is only worth showing to a member who had a watchlist.
    var showsFailure: Bool {
        state == .failed && rows.isEmpty && memory.lastCount > 0
    }

    func refreshIfStale() async {
        if let loadedAt, clock().timeIntervalSince(loadedAt) < Self.staleAfter { return }
        await load()
    }

    /// Reads the watchlist. Quiet on failure when rows are already up.
    func load() async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            let response = try await dataSource.watchlist()
            apply(response.assets)
            if let market = response.market { afterHours = market.afterHours }
            loadedAt = clock()
            refreshFailed = false
            state = .loaded
        } catch {
            if error.isRequestCancellation { return }
            if rows.isEmpty {
                state = .failed
            } else {
                refreshFailed = true
            }
        }
    }

    /// Takes a stock off, at once, and puts it back if the server refuses.
    func remove(symbol: String) async {
        guard let index = rows.firstIndex(where: { $0.asset.symbol == symbol }) else { return }
        let removed = rows.remove(at: index)
        memory.lastCount = rows.count
        do {
            try await dataSource.remove(symbol: symbol)
            toast = MonacoToast(message: WatchlistCopy.removed, isSuccess: true)
        } catch {
            if error.isRequestCancellation { return }
            rows.insert(removed, at: min(index, rows.count))
            memory.lastCount = rows.count
            toast = MonacoToast(message: WatchlistCopy.writeFailed)
        }
    }

    /// Saves a new order. `symbols` must be a permutation of what is on screen; the rows
    /// move at once and move back if the server refuses. A 409 means the list changed on
    /// another device, so the model reloads and says so.
    func reorder(to symbols: [String]) async {
        let previous = rows
        guard symbols != previous.map(\.asset.symbol),
              Set(symbols) == Set(previous.map(\.asset.symbol)) else { return }
        let bySymbol = Dictionary(uniqueKeysWithValues: previous.map { ($0.asset.symbol, $0) })
        rows = symbols.compactMap { bySymbol[$0] }
        do {
            _ = try await dataSource.reorder(symbols: symbols)
        } catch {
            if error.isRequestCancellation { return }
            rows = previous
            if WatchlistWriteFailure(error, isReorder: true) == .changed {
                toast = MonacoToast(message: WatchlistCopy.changed)
                await load()
            } else {
                toast = MonacoToast(message: WatchlistCopy.writeFailed)
            }
        }
    }

    private func apply(_ assets: [MarketAssetDTO]) {
        rows = assets.map { MarketRowData(asset: $0) }
        memory.lastCount = rows.count
        if !rows.isEmpty { memory.hasWatched = true }
    }
}
