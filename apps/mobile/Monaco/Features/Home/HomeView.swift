import Charts
import MonacoCore
import SwiftUI

/// Home dashboard: net worth, my cabals, P&L chart, leaderboard, missed proposals.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    var onRefresh: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @State private var dashboard: HomeDashboardDTO?
    @State private var leaderboardRange: HomeLeaderboardRange = .all
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        Group {
            if isLoading, dashboard == nil {
                ProgressView("Loading home…")
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else if let dashboard {
                dashboardScroll(dashboard)
            } else if let errorMessage {
                VStack(alignment: .leading, spacing: 12) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.destructive)
                    Button("Try again") {
                        Task { await loadDashboard() }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding()
                .monacoSurfaceCard()
                .padding()
            }
        }
        .background(MonacoTheme.background)
        .navigationTitle("Home")
        .task {
            await loadDashboard()
        }
        .refreshable {
            await loadDashboard()
            await onRefresh()
        }
        .onChange(of: leaderboardRange) { _, _ in
            Task { await reloadLeaderboard() }
        }
    }

    @ViewBuilder
    private func dashboardScroll(_ dashboard: HomeDashboardDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                netWorthSection(dashboard)
                myGroupsSection(dashboard)
                pnlChartSection(dashboard)
                leaderboardSection(dashboard)
                missedProposalsSection(dashboard)
            }
            .padding()
        }
    }

    private func netWorthSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Net worth")
                .font(.caption.bold())
                .foregroundStyle(MonacoTheme.secondaryText)
            Text("$\(dashboard.netWorthUsd)")
                .font(.largeTitle.bold().monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
            HStack(spacing: 12) {
                Text(dashboard.netWorthDollarPnl)
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(pnlColor(for: dashboard.netWorthDollarPnl))
                Text(PercentReturnFormatter.format(dashboard.netWorthPercentReturn))
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(MonacoTheme.secondaryText)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .monacoSurfaceCard()
        .accessibilityIdentifier("home-net-worth")
    }

    private func myGroupsSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("My cabals")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            if dashboard.myGroups.isEmpty {
                MonacoEmptyStateCard(
                    message: "No cabals yet. Create or join one from the Groups tab.",
                    systemImage: "person.3"
                )
            } else {
                ForEach(dashboard.myGroups) { row in
                    NavigationLink {
                        GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(row.name)
                                    .font(.body.bold())
                                    .foregroundStyle(MonacoTheme.primaryText)
                                Text("Your position $\(row.equityUsd)")
                                    .font(.caption)
                                    .foregroundStyle(MonacoTheme.secondaryText)
                            }
                            Spacer()
                            VStack(alignment: .trailing, spacing: 2) {
                                Text(SlicePercentFormatter.format(row.slicePercent))
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(MonacoTheme.secondaryText)
                                Text(PercentReturnFormatter.format(row.percentReturn))
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(MonacoTheme.primaryText)
                                Text(row.dollarPnl)
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(pnlColor(for: row.dollarPnl))
                            }
                        }
                        .padding(.vertical, 4)
                    }
                    .accessibilityIdentifier("home-my-group-\(row.groupId)")
                }
            }
        }
        .padding()
        .monacoSurfaceCard()
    }

    private func pnlChartSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Hourly P&L")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            if dashboard.pnlSeries1H.count < 2 {
                MonacoEmptyStateCard(
                    message: "No recent P&L history yet. Activity will appear here after deposits or trades.",
                    systemImage: "chart.line.uptrend.xyaxis"
                )
            } else {
                Chart(dashboard.pnlSeries1H) { point in
                    AreaMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(MonacoTheme.accent.opacity(0.25))
                    LineMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(MonacoTheme.accent)
                }
                .chartYAxis {
                    AxisMarks(position: .leading)
                }
                .frame(height: 180)
            }
        }
        .padding()
        .monacoSurfaceCard()
        .accessibilityIdentifier("home-pnl-chart")
    }

    private func leaderboardSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Leaderboard")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            Picker("Range", selection: $leaderboardRange) {
                ForEach(HomeLeaderboardRange.allCases, id: \.self) { range in
                    Text(range.label).tag(range)
                }
            }
            .pickerStyle(.segmented)
            .monacoSegmentedBoardPicker()

            if dashboard.leaderboard.people.isEmpty {
                MonacoEmptyStateCard(
                    message: "No leaderboard rows yet. Join a funded cabal to compete.",
                    systemImage: "chart.bar"
                )
            } else {
                ForEach(dashboard.leaderboard.people) { row in
                    NavigationLink {
                        UserProfileGroupsView(
                            auth: auth,
                            userId: row.userId,
                            displayName: row.displayName
                        )
                    } label: {
                        HStack {
                            Text(row.displayName)
                                .font(.body.bold())
                                .foregroundStyle(MonacoTheme.primaryText)
                            Spacer()
                            boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
                        }
                        .padding(.vertical, 4)
                    }
                    .accessibilityIdentifier("home-leaderboard-row-\(row.userId)")
                }
            }
        }
        .padding()
        .monacoSurfaceCard()
    }

    private func missedProposalsSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Missed proposals")
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)

            if dashboard.missedProposals.isEmpty {
                MonacoEmptyStateCard(
                    message: "You're caught up. No open proposals waiting for your vote.",
                    systemImage: "checkmark.circle"
                )
            } else {
                ForEach(dashboard.missedProposals) { row in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.groupName)
                                .font(.caption)
                                .foregroundStyle(MonacoTheme.secondaryText)
                            HStack {
                                Text(AssetSymbolFormatter.format(row.symbol))
                                    .font(.body.bold())
                                    .foregroundStyle(MonacoTheme.primaryText)
                                Spacer()
                                Text("Vote")
                                    .font(.caption.bold())
                                    .foregroundStyle(MonacoTheme.accent)
                            }
                        }
                        .padding(.vertical, 4)
                    }
                    .accessibilityIdentifier("home-missed-proposal-\(row.proposalId)")
                }
            }
        }
        .padding()
        .monacoSurfaceCard()
    }

    @ViewBuilder
    private func boardMetrics(percentReturn: String?, dollarPnl: String) -> some View {
        VStack(alignment: .trailing, spacing: 2) {
            Text(PercentReturnFormatter.format(percentReturn))
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(percentReturn == nil ? MonacoTheme.secondaryText : MonacoTheme.primaryText)
            Text(dollarPnl)
                .font(.caption.monospacedDigit())
                .foregroundStyle(pnlColor(for: dollarPnl))
        }
    }

    private func pnlColor(for dollarPnl: String) -> Color {
        if dollarPnl.hasPrefix("-") {
            return MonacoTheme.warning
        }
        if dollarPnl.hasPrefix("+") && dollarPnl != "+0.00" {
            return MonacoTheme.success
        }
        return MonacoTheme.secondaryText
    }

    private func loadDashboard() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        isLoading = dashboard == nil
        errorMessage = nil

        do {
            dashboard = try await apiClient.getHomeDashboard(
                accessToken: accessToken,
                leaderboardRange: leaderboardRange
            )
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch {
            errorMessage = "Could not load home dashboard."
            dashboard = nil
        }

        isLoading = false
    }

    private func reloadLeaderboard() async {
        guard let accessToken = auth.accessToken else { return }
        guard let current = dashboard else {
            await loadDashboard()
            return
        }
        do {
            let updated = try await apiClient.getHomeDashboard(
                accessToken: accessToken,
                leaderboardRange: leaderboardRange
            )
            dashboard = HomeDashboardDTO(
                netWorthUsd: current.netWorthUsd,
                netWorthDollarPnl: current.netWorthDollarPnl,
                netWorthPercentReturn: current.netWorthPercentReturn,
                myGroups: current.myGroups,
                pnlSeries1H: current.pnlSeries1H,
                leaderboard: updated.leaderboard,
                missedProposals: current.missedProposals
            )
        } catch {
            // Keep existing dashboard; pull-to-refresh can retry.
        }
    }
}

#Preview {
    NavigationStack {
        HomeView(auth: PrivyAuthService())
            .monacoRootAppearance()
    }
}
