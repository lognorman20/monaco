import Foundation
import MonacoCore

/// Reads and writes the member's watchlist. The live source calls the API; tests and the
/// sample harness swap in a stub.
@MainActor
protocol WatchlistDataSource {
    func watchlist() async throws -> WatchlistResponseDTO
    func add(symbol: String) async throws
    func remove(symbol: String) async throws
    /// Every symbol on the watchlist, once, in the new order. Answers the order the server kept.
    func reorder(symbols: [String]) async throws -> [String]
}

/// Reads and writes the member's price alerts.
@MainActor
protocol PriceAlertDataSource {
    /// Every alert, or one stock's when `symbol` is set.
    func alerts(symbol: String?) async throws -> PriceAlertsResponseDTO
    func createAlert(symbol: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) async throws -> PriceAlertDTO
    func deleteAlert(id: String) async throws
}

/// The API behind both. Requests and DTOs live in MonacoCore; this adds the session token,
/// and `withAccessToken` ends the session on a 401.
@MainActor
struct LiveWatchlistDataSource: WatchlistDataSource, PriceAlertDataSource {
    let auth: PrivyAuthService

    private func client(_ token: String) -> MonacoCore.MonacoAPIClient {
        MonacoCore.MonacoAPIClient(baseURL: Config.apiBaseURL, accessTokenProvider: { token })
    }

    func watchlist() async throws -> WatchlistResponseDTO {
        try await auth.withAccessToken { try await client($0).getWatchlist() }
    }

    func add(symbol: String) async throws {
        try await auth.withAccessToken { _ = try await client($0).addToWatchlist(symbol: symbol) }
    }

    func remove(symbol: String) async throws {
        try await auth.withAccessToken { try await client($0).removeFromWatchlist(symbol: symbol) }
    }

    func reorder(symbols: [String]) async throws -> [String] {
        try await auth.withAccessToken { try await client($0).reorderWatchlist(symbols: symbols) }
    }

    func alerts(symbol: String?) async throws -> PriceAlertsResponseDTO {
        try await auth.withAccessToken { try await client($0).listPriceAlerts(symbol: symbol) }
    }

    func createAlert(symbol: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) async throws -> PriceAlertDTO {
        try await auth.withAccessToken {
            try await client($0).createPriceAlert(symbol: symbol, direction: direction, priceUsdcMicros: lineUsdcMicros)
        }
    }

    func deleteAlert(id: String) async throws {
        try await auth.withAccessToken { try await client($0).deletePriceAlert(id: id) }
    }
}

extension Notification.Name {
    /// Posted after the member stars or unstars a stock, so a Stocks tab further down the
    /// navigation stack reloads its watchlist without the two screens knowing each other.
    static let monacoWatchlistDidChange = Notification.Name("monaco.watchlist.didChange")
}

/// What the Stocks tab remembers between launches about the member's watchlist: how many rows
/// to hold room for while it loads, and whether they have ever followed a stock (the first-use
/// hint stops once they have).
@MainActor
protocol WatchlistMemory: AnyObject {
    var lastCount: Int { get set }
    var hasWatched: Bool { get set }
}

@MainActor
final class DefaultsWatchlistMemory: WatchlistMemory {
    private let defaults: UserDefaults
    private static let lastCountKey = "monaco.watchlist.lastCount"
    private static let hasWatchedKey = "monaco.watchlist.hasWatched"

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    var lastCount: Int {
        get { defaults.integer(forKey: Self.lastCountKey) }
        set { defaults.set(max(0, newValue), forKey: Self.lastCountKey) }
    }

    var hasWatched: Bool {
        get { defaults.bool(forKey: Self.hasWatchedKey) }
        set { defaults.set(newValue, forKey: Self.hasWatchedKey) }
    }
}

/// Memory that forgets on relaunch: tests and the sample harness.
@MainActor
final class InMemoryWatchlistMemory: WatchlistMemory {
    var lastCount: Int
    var hasWatched: Bool

    init(lastCount: Int = 0, hasWatched: Bool = false) {
        self.lastCount = lastCount
        self.hasWatched = hasWatched
    }
}
