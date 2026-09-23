import MonacoCore
import Observation
import os
import SwiftUI

/// Which window the hero curve is drawing, and the series for it.
///
/// The hero used to be stuck on 1H and apologise for it in a caption. `GET /v1/home/pnl-series`
/// already takes a `range`, so the pills on the slab ask for the window the member picked.
///
/// The store's `homePnLSeries` stays the source for 1H — it is refreshed by every poll and by
/// pull-to-refresh — so the fold paints the curve it already has on the first frame and only ever
/// goes to the network for a window nobody has asked for yet. Windows are cached for the life of
/// the screen: flicking back to 1W must not re-fetch what is already drawn.
///
/// A failed window leaves the pill selected and the slot reserved rather than silently falling
/// back to another range's curve — a shape labelled with the wrong window is a lie about money.
@Observable
@MainActor
final class HomeHeroRangeModel {
    private(set) var range: HomeLeaderboardRange = .oneHour
    private(set) var isLoading = false
    /// True when the selected window came back but could not be loaded. The slot stays reserved.
    private(set) var failed = false

    private var cache: [HomeLeaderboardRange: [HomePnLSeriesPointDTO]] = [:]
    private var loadTask: Task<Void, Never>?

    /// The series for the selected window, or nil while it is still on its way.
    ///
    /// - Parameter oneHourSeries: the store's own 1H read, so the two never diverge.
    func series(oneHourSeries: [HomePnLSeriesPointDTO]?) -> [HomePnLSeriesPointDTO]? {
        range == .oneHour ? oneHourSeries : cache[range]
    }

    /// A pill tap. Re-tapping the selected pill is a no-op.
    func select(_ next: HomeLeaderboardRange, accessToken: String?, client: AppSessionDataSource) {
        guard next != range else { return }
        range = next
        failed = false
        load(accessToken: accessToken, client: client)
    }

    /// Loads the selected window if it is not already in hand. Ranges are immutable windows over
    /// history, so one read each is enough for a session on this screen.
    func load(accessToken: String?, client: AppSessionDataSource) {
        loadTask?.cancel()
        let wanted = range
        guard wanted != .oneHour, cache[wanted] == nil else {
            isLoading = false
            return
        }
        guard let token = accessToken else {
            // No token is not a failed read: the gate is about to replace this screen.
            isLoading = false
            return
        }
        isLoading = true
        loadTask = Task { [weak self] in
            do {
                let series = try await client.getHomePnLSeries(accessToken: token, range: wanted)
                guard let self, !Task.isCancelled else { return }
                cache[wanted] = series.points
                if range == wanted { isLoading = false }
            } catch {
                guard let self, !Task.isCancelled, !error.isRequestCancellation else { return }
                AppLogger.session.error(
                    "Home hero curve: \(wanted.rawValue, privacy: .public) failed to load"
                )
                if range == wanted {
                    isLoading = false
                    failed = true
                }
            }
        }
    }
}
