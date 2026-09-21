import MonacoCore
import Observation
import os
import SwiftUI

/// The dashboard read behind Home's "Top investors" board. `AppSessionStore` owns the
/// payload, so the live source drives the store and reads the range back off it; tests
/// swap in a stub.
@MainActor
protocol HomeLeaderboardDashboardSource {
    /// The range the dashboard currently on screen was built with, as the server echoed it.
    var loadedRange: HomeLeaderboardRange? { get }

    /// The range the store keeps as the member's pick, when it keeps one. The store outlives
    /// Home: a new sign-in or a rebuilt tab makes a fresh model, which starts from this
    /// rather than from all-time. Nil when the store keeps no selection of its own.
    var ownedRange: HomeLeaderboardRange? { get }

    /// Reloads the dashboard for `range`. Returns once the store has written the payload —
    /// or given up, which shows as `loadedRange` still naming the old range.
    func loadDashboard(range: HomeLeaderboardRange) async
}

@MainActor
struct LiveHomeLeaderboardDashboardSource: HomeLeaderboardDashboardSource {
    let auth: DynamicAuthService
    let session: AppSessionStore

    var loadedRange: HomeLeaderboardRange? {
        session.dashboard.flatMap { HomeLeaderboardRange(rawValue: $0.leaderboard.range) }
    }

    /// `AppSessionStore` on this build keeps only the range its last dashboard was read with,
    /// privately, and a refresh from any other tab resets that to all-time, so it is not the
    /// member's pick. The session-store range owner (`AppSessionStore.leaderboardRange`) is
    /// what this reads once the store has it.
    var ownedRange: HomeLeaderboardRange? { nil }

    func loadDashboard(range: HomeLeaderboardRange) async {
        await session.refreshDashboard(auth: auth, leaderboardRange: range)
    }
}

/// Owns which leaderboard range Home is showing.
///
/// The board itself lives on the shared dashboard, which any tab can replace: a pull to
/// refresh on Profile, creating a cabal, or a bootstrap after the access token rotates all
/// reload it with the default all-time board. The chip would then keep saying 1W over
/// all-time rows (#275). This model keeps the member's choice, compares it with the range
/// the server echoed back in the payload, and reloads when the two drift apart, so the rows
/// always describe the highlighted chip.
///
/// Range changes are one cancellable task (#276): tapping through 1H/1D/1W cancels the
/// superseded read, so a slow response can no longer land after a newer one. While a read is
/// in flight the board says so, and when it fails the board offers a retry instead of
/// labelling the old range's rows with the new chip.
@Observable
@MainActor
final class HomeLeaderboardModel {
    private(set) var selectedRange: HomeLeaderboardRange = .all
    private(set) var isLoading = false
    private(set) var failed = false

    private var loadTask: Task<Void, Never>?

    /// A chip tap. Re-tapping the selected chip is a no-op; `reconcile` is what recovers a
    /// board that drifted, so there is nothing to re-request here.
    func select(_ range: HomeLeaderboardRange, from source: HomeLeaderboardDashboardSource) {
        guard range != selectedRange else { return }
        selectedRange = range
        failed = false
        reload(from: source)
    }

    /// Takes the store's selection as this model's own, so the chip on a Home that was just
    /// built says what the store will keep asking the server for. A read in flight is left
    /// alone: it carries the member's newest pick, which the store is about to record.
    func adoptOwnedRange(from source: HomeLeaderboardDashboardSource) {
        guard !isLoading, let owned = source.ownedRange, owned != selectedRange else { return }
        selectedRange = owned
        failed = false
    }

    func retry(from source: HomeLeaderboardDashboardSource) {
        failed = false
        reload(from: source)
    }

    /// Called when the dashboard changes under Home. A payload built for another range means
    /// someone else's refresh reset the board, so ask for the member's range back. A payload
    /// that already matches clears an earlier failure.
    ///
    /// Nothing is re-requested while a read is in flight or after one failed, so a server that
    /// keeps answering with a different range cannot spin this into a loop.
    func reconcile(from source: HomeLeaderboardDashboardSource) {
        // An echo this build cannot read says nothing about whether the board drifted, so the
        // member's choice and any existing state stand.
        guard let loaded = source.loadedRange else { return }
        guard loaded != selectedRange else {
            failed = false
            return
        }
        guard !isLoading, !failed else { return }
        reload(from: source)
    }

    private func reload(from source: HomeLeaderboardDashboardSource) {
        let requested = selectedRange
        loadTask?.cancel()
        isLoading = true
        loadTask = Task { [weak self] in
            await self?.load(requested, from: source)
        }
    }

    private func load(_ requested: HomeLeaderboardRange, from source: HomeLeaderboardDashboardSource) async {
        await source.loadDashboard(range: requested)
        // A newer tap superseded this one: its own load owns the state now.
        guard !Task.isCancelled, requested == selectedRange else { return }
        isLoading = false
        // The store swallows its own read failures, so the range echoed in the payload is the
        // only proof the read landed. An echo this build cannot read is not that proof either
        // way — a range added server-side, or an echo the backend starts normalising, would
        // otherwise put every member on "Couldn't load the board" for a payload that arrived
        // perfectly well. Unknown means leave the board alone, not show an error.
        guard let loaded = source.loadedRange else {
            failed = false
            AppLogger.session.error(
                "Home leaderboard: dashboard echoed a range this build cannot read; treating the \(requested.rawValue, privacy: .public) read as unknown"
            )
            return
        }
        failed = loaded != requested
        if failed {
            AppLogger.session.error(
                "Home leaderboard: asked for \(requested.rawValue, privacy: .public), dashboard came back as \(loaded.rawValue, privacy: .public)"
            )
        }
    }
}

extension HomeLeaderboardRange {
    /// Spoken chip label. "1 H" is what VoiceOver makes of the printed one.
    var accessibilityName: String {
        switch self {
        case .oneHour: "Past hour"
        case .oneDay: "Past day"
        case .oneWeek: "Past week"
        case .oneMonth: "Past month"
        case .all: "All time"
        }
    }

    /// The window in the empty-board copy: "no returns to rank for the past week yet".
    var windowPhrase: String {
        switch self {
        case .oneHour: "the past hour"
        case .oneDay: "the past day"
        case .oneWeek: "the past week"
        case .oneMonth: "the past month"
        case .all: "all time"
        }
    }
}
