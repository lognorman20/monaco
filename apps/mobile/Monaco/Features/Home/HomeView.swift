import SwiftUI

/// Home dashboard. Order: hero → balance row → "Needs your vote" (if any) →
/// "Your cabals" → chart (if ≥ 3 points) → "Top investors". The hero is the title —
/// no large nav title competes with it.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    @Binding var selectedTab: MainTab
    @Environment(AppSessionStore.self) private var session

    @State private var leaderboardRange: HomeLeaderboardRange = .all

    private var joinedCabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    var body: some View {
        Group {
            // Skeleton until the dashboard lands (#217: session and dashboard load separately).
            if session.dashboard == nil, session.errorMessage == nil {
                HomeSkeletonView()
            } else if let dashboard = session.dashboard {
                dashboardScroll(dashboard)
            } else if let errorMessage = session.errorMessage {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    Text(errorMessage)
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.destructive)
                    Button("Try again") {
                        Task { await session.refresh(auth: auth, leaderboardRange: leaderboardRange) }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding(MonacoTheme.Space.m)
            } else {
                dashboardScroll(
                    HomeDashboardDTO(
                        netWorthUsd: "0.00",
                        netWorthDollarPnl: "+0.00",
                        netWorthPercentReturn: nil,
                        myGroups: [],
                        pnlSeries1H: [],
                        leaderboard: HomeLeaderboardSectionDTO(range: "ALL", people: []),
                        missedProposals: []
                    )
                )
            }
        }
        .monacoCanvas()
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await session.refresh(auth: auth, leaderboardRange: leaderboardRange)
        }
        .onChange(of: leaderboardRange) { _, range in
            Task { await session.refreshDashboard(auth: auth, leaderboardRange: range) }
        }
    }

    private func dashboardScroll(_ dashboard: HomeDashboardDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                HomeNetWorthSection(dashboard: dashboard)

                HomeBalanceRowSection(
                    auth: auth,
                    balance: session.platformBalance,
                    isBalanceLoading: session.isBalanceLoading,
                    joinedCabals: joinedCabals
                )

                if !dashboard.missedProposals.isEmpty {
                    HomeMissedVotesSection(
                        auth: auth,
                        rows: dashboard.missedProposals
                    )
                }

                HomePositionsSection(
                    auth: auth,
                    rows: dashboard.myGroups,
                    onLeft: { await session.refresh(auth: auth, leaderboardRange: leaderboardRange) },
                    onBrowseCabals: { selectedTab = .cabals }
                )

                // The 1H series loads after first paint (#217); a flat line under three points reads as broken.
                let pnlPoints = session.homePnLSeries ?? dashboard.pnlSeries1H
                if session.isHomePnLSeriesLoading, session.homePnLSeries == nil {
                    RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                        .fill(MonacoTheme.surface)
                        .frame(height: 160)
                        .accessibilityLabel("Loading chart")
                        .accessibilityIdentifier("home-pnl-chart-loading")
                } else if pnlPoints.count >= 3 {
                    HomePnLChartSection(points: pnlPoints)
                }

                HomeLeaderboardSection(
                    auth: auth,
                    range: $leaderboardRange,
                    people: dashboard.leaderboard.people
                )
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
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
