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

/// Home dashboard. Order: **the ink fold** (money, curve, window pills, cash) → "Needs your vote"
/// on Home's one ink band → "Your cabals" → "Top investors". The hero is the title; past 140pt the
/// figure hands off into the nav bar so nothing competes with it.
struct HomeView: View {
    @ObservedObject var auth: DynamicAuthService
    @Binding var selectedTab: MainTab
    @Environment(AppSessionStore.self) private var session

    @State private var leaderboard = HomeLeaderboardModel()
    @State private var heroRange = HomeHeroRangeModel()
    @State private var isRetrying = false
    /// True once the fold has scrolled past; the figure hands off into the nav bar (§4 #21).
    @State private var heroScrolledAway = false
    @State private var toast: MonacoToast?
    /// Advanced only when a vote actually closes, so "Needs your vote" can drop an expired row
    /// between dashboard polls without putting the whole screen on a 60-second timer.
    @State private var votesClock = Date()
    /// The proposal pushed from "Needs your vote". Held here, at the tab root, so the pushed
    /// screen outlives both the countdown's tick and the row's own expiry.
    @State private var openProposalId: String?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// The nav-title handoff crossfade (§4 #21).
    private static let handoff = Animation.easeInOut(duration: 0.20)

    private var joinedCabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    private var potValuesUsd: [String: String] {
        Dictionary(joinedCabals.map { ($0.groupId, $0.potValueUsd) }, uniquingKeysWith: { first, _ in first })
    }

    private var leaderboardSource: LiveHomeLeaderboardDashboardSource {
        LiveHomeLeaderboardDashboardSource(auth: auth, session: session)
    }

    /// Chunk E's vote path, consumed read-only, so the deck at the top of Home posts the same
    /// ballot the proposal screen does rather than growing a second one of its own.
    private var voteService: LiveProposalFeedService {
        LiveProposalFeedService(auth: auth)
    }

    /// The nav title once the fold has scrolled away. Empty while the figure is on screen.
    private var handoffTitle: String {
        guard heroScrolledAway, let dashboard = session.dashboard else { return "" }
        return UsdAmountFormatter.format(decimalString: dashboard.netWorthUsd)
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
                // The ink chrome belongs to the fold, not to Home. It sits on this branch and
                // nowhere else: the skeleton and the failed screen are paper on a paper canvas,
                // and a solid near-black bar floating over them in light mode — the first frame
                // of every cold launch, and the whole offline state — is not the fold, it is a
                // mistake. Under the fold the bar is the slab's own chrome, in both schemes, so
                // ink is continuous from the status bar down; it is also where the figure lands
                // once the fold has scrolled away (§4 #21).
                dashboardScroll(dashboard)
                    .toolbarBackground(MonacoTheme.Ink.base, for: .navigationBar)
                    .toolbarBackground(.visible, for: .navigationBar)
                    .toolbarColorScheme(.dark, for: .navigationBar)
            case .failed(let message):
                failedScroll(message)
            }
        }
        .monacoCanvas()
        .navigationTitle(handoffTitle)
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
        .onAppear {
            // A Home built after the store already holds a board: take the store's pick, then
            // make sure the rows on screen are that range's.
            leaderboard.adoptOwnedRange(from: leaderboardSource)
            leaderboard.reconcile(from: leaderboardSource)
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
            VStack(alignment: .leading, spacing: 0) {
                // The ink fold: full-bleed, under the status bar, square top corners and a 28pt
                // bottom radius, so paper slides up over its bottom edge. The curve's slot is
                // sized from the dashboard so the layout does not move when a series lands (#217).
                HomeNetWorthSection(
                    dashboard: dashboard,
                    chart: HomeHeroChart.resolve(
                        loaded: heroRange.series(oneHourSeries: session.homePnLSeries),
                        // The dashboard's embedded copy is a 1H series; it is not an answer about
                        // any other window.
                        embedded: heroRange.range == .oneHour ? dashboard.pnlSeries1H : [],
                        hasCabals: !dashboard.myGroups.isEmpty
                    ),
                    range: Binding(
                        get: { heroRange.range },
                        set: { heroRange.select($0, accessToken: auth.accessToken, client: MonacoAPIClient()) }
                    ),
                    isRangeLoading: heroRange.isLoading,
                    rangeFailed: heroRange.failed,
                    balanceFold: HomeBalanceFold(
                        auth: auth,
                        balance: session.platformBalance,
                        isBalanceLoading: session.isBalanceLoading,
                        joinedCabals: joinedCabals,
                        isRetryingBalance: isRetrying,
                        // Shares `retryLoad`'s in-flight guard: retrying the balance is the same
                        // three-request refresh, so it cannot be stacked by tapping repeatedly.
                        onRetryBalance: { Task { await retryLoad() } }
                    ),
                    isHandedOff: heroScrolledAway
                )
                // `.monacoInkSlab()` cancels the screen gutter from the inside, so the slab has
                // to sit inside one. Without this its content hangs 20pt off both edges.
                .padding(.horizontal, MonacoTheme.Space.gutter)

                VStack(alignment: .leading, spacing: MonacoTheme.Space.section) {
                    // Gated on the rows still open rather than on the payload: a section that
                    // renders nothing still takes a `VStack` spacing on each side, which would
                    // leave a doubled gap here until the next dashboard write.
                    let openVotes = HomeMissedVotes.open(dashboard.missedProposals, now: votesClock)
                    if !openVotes.isEmpty {
                        HomeMissedVotesSection(
                            rows: openVotes,
                            onOpen: { openProposalId = $0 },
                            onVote: { row, choice in await castVote(choice, on: row) }
                        )
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
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.top, firstSectionClearance(dashboard))
                .padding(.bottom, MonacoTheme.Space.l)
            }
        }
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y + geometry.contentInsets.top > 140
        } action: { _, scrolledAway in
            // Keyed on the Bool, not on the offset, so the crossfade cannot chatter while a
            // finger hovers on the threshold (§4 #21). The figure fades out as the nav title
            // fades in, both over 0.20s; under Reduce Motion it is an instant swap.
            guard scrolledAway != heroScrolledAway else { return }
            withAnimation(Self.handoff.reduced(reduceMotion)) {
                heroScrolledAway = scrolledAway
            }
        }
    }

    /// What comes under the fold takes the section rhythm — unless it is the ink band, which
    /// already carries 40pt of clearance of its own on both sides.
    private func firstSectionClearance(_ dashboard: HomeDashboardDTO) -> CGFloat {
        HomeMissedVotes.open(dashboard.missedProposals, now: votesClock).isEmpty
            ? MonacoTheme.Space.section
            : 0
    }

    /// Posts a ballot from the deck at the top of Home, then refreshes: the row disappears
    /// because the server dropped it, not because this screen guessed that it would.
    private func castVote(_ choice: ProposalVoteChoice, on row: HomeMissedProposalRowDTO) async -> Bool {
        let outcome = await ProposalVoting.cast(choice, proposalId: row.proposalId, service: voteService)
        toast = outcome.toast
        if outcome.succeeded {
            Haptics.success()
            await refreshHome()
        }
        return outcome.succeeded
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
                        .tint(MonacoTheme.controlTint)
                        .accessibilityLabel("Loading")
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, MonacoTheme.Space.gutter)
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

/// Skeleton fold + three rows, per the plan's Home loading spec.
private struct HomeSkeletonView: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 140, height: 14)
                    SkeletonBlock(width: 180, height: 44)
                }

                SkeletonBlock(height: 64, radius: MonacoTheme.Radius.container)

                VStack(spacing: MonacoTheme.Space.s) {
                    ForEach(0..<3, id: \.self) { _ in
                        SkeletonBlock(height: 60, radius: MonacoTheme.Radius.container)
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
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
        HomeView(auth: DynamicAuthService(), selectedTab: .constant(.home))
            .environment(session)
            .monacoRootAppearance()
    }
}
