import SwiftUI

/// Home dashboard: net worth, positions, P&L chart, leaderboard, missed votes.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    @State private var leaderboardRange: HomeLeaderboardRange = .all

    private var joinedCabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    var body: some View {
        Group {
            if session.isLoading, session.dashboard == nil {
                ProgressView("Loading home…")
                    .tint(MonacoTheme.accent)
                    .foregroundStyle(MonacoTheme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
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
        .navigationTitle("Home")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    NavigationLink {
                        CreateGroupView(auth: auth)
                    } label: {
                        Label("Create cabal", systemImage: "plus")
                    }
                    NavigationLink {
                        JoinGroupView(auth: auth)
                    } label: {
                        Label("Join cabal", systemImage: "person.badge.plus")
                    }
                } label: {
                    Image(systemName: "plus.circle")
                        .monacoToolbarIcon()
                }
                .accessibilityIdentifier("home-club-menu")
            }
        }
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
                HomeNetWorthSection(
                    dashboard: dashboard,
                    balance: session.platformBalance,
                    isBalanceLoading: session.isBalanceLoading
                ) {
                    NavigationLink {
                        DepositView(auth: auth, joinedCabals: joinedCabals)
                    } label: {
                        Text("Deposit")
                    }
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("home-deposit-link")
                }

                HomePositionsSection(
                    auth: auth,
                    rows: dashboard.myGroups,
                    onLeft: { await session.refresh(auth: auth, leaderboardRange: leaderboardRange) }
                )

                if session.isHomePnLSeriesLoading, session.homePnLSeries == nil {
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        Text("P&L · last hour")
                            .font(MonacoTheme.TypeRole.title)
                            .foregroundStyle(MonacoTheme.ink)
                        ProgressView()
                            .tint(MonacoTheme.accent)
                            .frame(maxWidth: .infinity, minHeight: 160)
                            .accessibilityIdentifier("home-pnl-chart-loading")
                    }
                } else {
                    HomePnLChartSection(points: session.homePnLSeries ?? dashboard.pnlSeries1H)
                }

                HomeLeaderboardSection(
                    auth: auth,
                    range: $leaderboardRange,
                    people: dashboard.leaderboard.people
                )

                HomeMissedVotesSection(
                    auth: auth,
                    rows: dashboard.missedProposals
                )
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.l)
        }
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
        HomeView(auth: PrivyAuthService())
            .environment(session)
            .monacoRootAppearance()
    }
}
