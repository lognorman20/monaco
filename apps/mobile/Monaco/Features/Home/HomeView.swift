import MonacoCore
import os
import SwiftUI

/// What Home is showing right now. One value instead of a ladder of optionals, so the
/// screen cannot fall through to a fabricated "$0.00" dashboard (#327) and every state has
/// exactly one branch.
enum HomeScreenState: Equatable {
    case loading
    case loaded(HomeDashboardDTO)
    case failed(String)

    /// Data wins over an error: once a poll lands the board is real, and a refresh that fails
    /// with the board on screen is reported by the toast in `body`, not by replacing it (#278).
    static func resolve(dashboard: HomeDashboardDTO?, errorMessage: String?) -> HomeScreenState {
        if let dashboard { return .loaded(dashboard) }
        if let errorMessage { return .failed(errorMessage) }
        return .loading
    }
}

/// Home dashboard. Order: hero → balance row → "Needs your vote" (if any) →
/// "Your cabals" → "Top investors". The hero is the title — no large nav title competes
/// with it.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    @Binding var selectedTab: MainTab
    @Environment(AppSessionStore.self) private var session

    @State private var leaderboard = HomeLeaderboardModel()
    @State private var isRetrying = false
    @State private var toast: MonacoToast?
    /// Advanced only when a vote actually closes, so "Needs your vote" can drop an expired row
    /// between dashboard polls without putting the whole screen on a 60-second timer.
    @State private var votesClock = Date()
    /// The proposal pushed from "Needs your vote". Held here, at the tab root, so the pushed
    /// screen outlives both the countdown's tick and the row's own expiry.
    @State private var openProposalId: String?

    private var joinedCabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    private var potValuesUsd: [String: String] {
        Dictionary(joinedCabals.map { ($0.groupId, $0.potValueUsd) }, uniquingKeysWith: { first, _ in first })
    }

    private var leaderboardSource: LiveHomeLeaderboardDashboardSource {
        LiveHomeLeaderboardDashboardSource(auth: auth, session: session)
    }

    /// The range the board on screen was built with, straight from the payload.
    private var loadedLeaderboardRange: String? {
        session.dashboard?.leaderboard.range
    }

    private var missedProposals: [HomeMissedProposalRowDTO] {
        session.dashboard?.missedProposals ?? []
    }

    /// Restarts the expiry wait whenever the dashboard brings a different set of closing times.
    private var missedVoteExpiries: [Date] {
        missedProposals.map(\.expiresAt)
    }

    private var balanceIsUnavailable: Bool {
        HomeBalanceDisplay.resolve(
            balance: session.platformBalance,
            isLoading: session.isBalanceLoading
        ) == .unavailable
    }

    var body: some View {
        Group {
            switch HomeScreenState.resolve(dashboard: session.dashboard, errorMessage: session.errorMessage) {
            case .loading:
                // Skeleton until the dashboard lands (#217: session and dashboard load separately).
                HomeSkeletonView()
            case .loaded(let dashboard):
                dashboardScroll(dashboard)
            case .failed(let message):
                failedScroll(message)
            }
        }
        .monacoCanvas()
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                profileButton
            }
        }
        .navigationDestination(item: $openProposalId) { proposalId in
            ProposalDetailView(auth: auth, proposalId: proposalId)
        }
        .refreshable {
            await pullToRefresh()
        }
        .onChange(of: loadedLeaderboardRange) { _, _ in
            leaderboard.reconcile(from: leaderboardSource)
        }
        .onChange(of: balanceIsUnavailable) { _, unavailable in
            if unavailable {
                AppLogger.session.error("Home: account balance unavailable — the balance read left it unset")
            }
        }
        .task(id: missedVoteExpiries) {
            await advanceVotesClock()
        }
        .pollWhileVisible(every: LiveRefreshCadence.resting) {
            try await session.pollLive(auth: auth)
        }
        .monacoToast($toast)
        .monacoFrameStats("Home")
    }

    /// The viewer's photo (or initials) in the corner; tapping it switches to the Profile tab.
    /// `MainTabView` plays the selection haptic for every tab change, this one included.
    private var profileButton: some View {
        Button {
            selectedTab = .profile
        } label: {
            MonacoAvatar(
                photoURL: session.me?.profilePhotoUrl,
                displayName: session.me?.displayName ?? "",
                size: 32
            )
            .frame(width: 44, height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Profile")
        .accessibilityIdentifier("home-profile-avatar")
    }

    private func dashboardScroll(_ dashboard: HomeDashboardDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                // The 1H series loads after first paint (#217); the slot is sized from the
                // dashboard so the layout does not move when it lands. `homePnLSeries` is nil
                // until that read finishes, which is what tells the slot to stay silent
                // instead of announcing an empty curve it has not asked about yet.
                HomeNetWorthSection(
                    dashboard: dashboard,
                    chart: HomeHeroChart.resolve(
                        loaded: session.homePnLSeries,
                        embedded: dashboard.pnlSeries1H,
                        hasCabals: !dashboard.myGroups.isEmpty
                    )
                )

                HomeBalanceRowSection(
                    auth: auth,
                    balance: session.platformBalance,
                    isBalanceLoading: session.isBalanceLoading,
                    joinedCabals: joinedCabals,
                    isRetryingBalance: isRetrying,
                    // Shares `retryLoad`'s in-flight guard: retrying the balance is the same
                    // three-request refresh, so it cannot be stacked by tapping repeatedly.
                    onRetryBalance: { Task { await retryLoad() } }
                )

                // Gated on the rows still open rather than on the payload: a section that
                // renders nothing still takes a `VStack` spacing on each side, which would
                // leave a doubled gap here until the next dashboard write.
                let openVotes = HomeMissedVotes.open(dashboard.missedProposals, now: votesClock)
                if !openVotes.isEmpty {
                    HomeMissedVotesSection(rows: openVotes, onOpen: { openProposalId = $0 })
                }

                HomePositionsSection(
                    auth: auth,
                    rows: dashboard.myGroups,
                    potValuesUsd: potValuesUsd,
                    onLeft: { await refreshHome() },
                    onBrowseCabals: { selectedTab = .cabals }
                )

                HomeLeaderboardSection(
                    auth: auth,
                    model: leaderboard,
                    people: dashboard.leaderboard.people,
                    hasCabals: !dashboard.myGroups.isEmpty,
                    onSelect: { leaderboard.select($0, from: leaderboardSource) },
                    onRetry: { leaderboard.retry(from: leaderboardSource) }
                )
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
    }

    /// The failed state lives in a ScrollView, so the "pull down to try again" the store asks
    /// for is a gesture this screen actually has (#278).
    private func failedScroll(_ message: String) -> some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.s) {
                // The store's sentence is the explanation, not the heading: as a title it read
                // "Couldn't load this. Pull down to try again." in bold, trailing period and
                // all, directly above a "Try again" button saying the same thing again.
                EmptyState(
                    title: "Couldn't load Home",
                    message: message,
                    actionTitle: "Try again",
                    action: { Task { await retryLoad() } }
                )
                .disabled(isRetrying)
                if isRetrying {
                    ProgressView()
                        .tint(MonacoTheme.ink)
                        .accessibilityLabel("Loading")
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.top, MonacoTheme.Space.xl)
        }
        .scrollBounceBehavior(.always)
        .accessibilityIdentifier("home-error")
    }

    private func retryLoad() async {
        guard !isRetrying else { return }
        isRetrying = true
        await refreshHome()
        isRetrying = false
    }

    private func pullToRefresh() async {
        await refreshHome()
        // A refresh the member let go of early is cancelled, and the store returns on
        // `isRequestCancellation` without touching `errorMessage` — so a message left over
        // from an earlier read would raise this toast for a read that merely stopped.
        // See "Needs from other areas": a per-refresh outcome would settle it properly.
        guard !Task.isCancelled else { return }
        // `refresh` clears the message when it succeeds, so anything left is this read failing.
        // With the board already on screen nothing else would say so.
        if session.dashboard != nil, session.errorMessage != nil {
            toast = MonacoToast(message: "Couldn't refresh just now")
        }
    }

    private func refreshHome() async {
        await session.refresh(auth: auth, leaderboardRange: leaderboard.selectedRange)
    }

    /// Sleeps until the next vote closes, then advances the clock the section is gated on.
    /// Waking on the expiry itself keeps an expired row from lingering until the next poll
    /// without re-evaluating Home every minute to check.
    private func advanceVotesClock() async {
        while !Task.isCancelled {
            guard let next = HomeMissedVotes.nextExpiry(missedProposals, after: votesClock) else { return }
            let wait = next.timeIntervalSinceNow
            if wait > 0 {
                do {
                    try await Task.sleep(for: .seconds(wait))
                } catch {
                    return
                }
            }
            votesClock = Date()
        }
    }
}

/// Skeleton hero + three rows, per the plan's Home loading spec.
private struct HomeSkeletonView: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 140, height: 14)
                    SkeletonBlock(width: 180, height: 44)
                }

                SkeletonBlock(height: 64, radius: MonacoTheme.Radius.card)

                VStack(spacing: MonacoTheme.Space.s) {
                    ForEach(0..<3, id: \.self) { _ in
                        SkeletonBlock(height: 60, radius: MonacoTheme.Radius.card)
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.top, MonacoTheme.Space.m)
        }
        .accessibilityIdentifier("home-loading")
    }
}

#Preview {
    let session = AppSessionStore()
    session.dashboard = HomeDashboardDTO(
        netWorthUsd: "1248.50",
        netWorthDollarPnl: "+48.20",
        netWorthPercentReturn: "0.040",
        myGroups: [
            HomeMyGroupRowDTO(
                groupId: "g1",
                name: "Weekend investors",
                equityUsd: "3.35",
                slicePercent: "0.12",
                dollarPnl: "+0.10",
                percentReturn: "0.031"
            ),
        ],
        pnlSeries1H: [],
        leaderboard: HomeLeaderboardSectionDTO(
            range: "ALL",
            people: [
                HomePeopleBoardRowDTO(
                    userId: "u1",
                    displayName: "Alfred",
                    percentReturn: "0.124",
                    dollarPnl: "+48.20"
                ),
            ]
        ),
        missedProposals: []
    )
    return NavigationStack {
        HomeView(auth: PrivyAuthService(), selectedTab: .constant(.home))
            .environment(session)
            .monacoRootAppearance()
    }
}
