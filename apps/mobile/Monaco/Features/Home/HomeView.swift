import Charts
import MonacoCore
import SwiftUI

/// Home dashboard: net worth, my cabals, P&L chart, leaderboard, missed proposals.
struct HomeView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var cabalCardWidth = 250.0
    @ObservedObject var auth: PrivyAuthService
    @Environment(\.monacoSessionRevision) private var sessionRevision
    var onRefresh: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @StateObject private var dashboardState = RefreshableSnapshot<HomeDashboardDTO>()
    private var dashboard: HomeDashboardDTO? { dashboardState.value }
    @State private var refreshToast: MonacoToast?
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
        .navigationTitle("Monaco")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($refreshToast)
        .task(id: "\(sessionRevision)-\(leaderboardRange.rawValue)") {
            await loadDashboard()
        }
        .refreshable {
            await onRefresh()
        }

    }

    private func dashboardScroll(_ dashboard: HomeDashboardDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                Text("Your portfolio")
                    .font(MonacoTheme.display(32))
                    .accessibilityAddTraits(.isHeader)
                    .foregroundStyle(MonacoTheme.primaryText)
                    .padding(.top, 8)

                VStack(alignment: .leading, spacing: 24) {
                    netWorthSection(dashboard)
                    pnlChartSection(dashboard)
                }
                .padding(24)
                .background(MonacoTheme.mint, in: RoundedRectangle(cornerRadius: 30))

                myGroupsSection(dashboard)
                leaderboardSection(dashboard)
                missedProposalsSection(dashboard)
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 28)
        }
    }

    private func netWorthSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("TOTAL NET WORTH")
                .font(.caption.weight(.semibold))
                .tracking(1.4)
                .foregroundStyle(MonacoTheme.secondaryText)
            Text("$\(dashboard.netWorthUsd)")
                .font(MonacoTheme.display(44).monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
                .minimumScaleFactor(0.55)
                .lineLimit(1)
                .contentTransition(.numericText())
                .animation(reduceMotion ? nil : .spring(response: 0.45, dampingFraction: 0.85), value: dashboard.netWorthUsd)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) { returnMetrics(dashboard) }
                VStack(alignment: .leading, spacing: 8) { returnMetrics(dashboard) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("home-net-worth")
    }

    @ViewBuilder
    private func returnMetrics(_ dashboard: HomeDashboardDTO) -> some View {
        Text(PercentReturnFormatter.format(dashboard.netWorthPercentReturn))
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(MonacoTheme.primaryText)
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .background(MonacoTheme.surface.opacity(0.8), in: Capsule())
        Text(dashboard.netWorthDollarPnl)
            .font(.subheadline.monospacedDigit())
            .foregroundStyle(pnlColor(for: dashboard.netWorthDollarPnl))
    }

    private func myGroupsSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            sectionHeading("My cabals", detail: "\(dashboard.myGroups.count)")

            if dashboard.myGroups.isEmpty {
                Text("Create or join a cabal in the Cabals tab to start investing with friends.")
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: 12) {
                        ForEach(dashboard.myGroups) { row in
                            NavigationLink {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                            } label: {
                                VStack(alignment: .leading, spacing: 18) {
                                    HStack {
                                        MonacoIdentityMark(title: row.name, size: 44)
                                        Spacer()
                                        Image(systemName: "arrow.up.right")
                                            .font(.subheadline.weight(.semibold))
                                            .foregroundStyle(MonacoTheme.secondaryText)
                                    }
                                    VStack(alignment: .leading, spacing: 6) {
                                        Text(row.name)
                                            .font(MonacoTheme.display(20))
                                            .foregroundStyle(MonacoTheme.primaryText)
                                            .lineLimit(2)
                                        Text("Your position")
                                            .font(.caption)
                                            .foregroundStyle(MonacoTheme.secondaryText)
                                        Text("$\(row.equityUsd)")
                                            .font(MonacoTheme.display(28).monospacedDigit())
                                            .foregroundStyle(MonacoTheme.primaryText)
                                            .lineLimit(1)
                                            .minimumScaleFactor(0.6)
                                    }
                                    Divider().overlay(MonacoTheme.border)
                                    HStack(alignment: .top) {
                                        VStack(alignment: .leading, spacing: 3) {
                                            Text(SlicePercentFormatter.format(row.slicePercent))
                                                .foregroundStyle(MonacoTheme.primaryText)
                                            Text("Ownership")
                                                .foregroundStyle(MonacoTheme.secondaryText)
                                        }
                                        Spacer(minLength: 8)
                                        VStack(alignment: .trailing, spacing: 3) {
                                            Text(PercentReturnFormatter.format(row.percentReturn))
                                                .foregroundStyle(MonacoTheme.primaryText)
                                            Text(row.dollarPnl)
                                                .foregroundStyle(pnlColor(for: row.dollarPnl))
                                        }
                                    }
                                    .font(.caption.monospacedDigit())
                                }
                                .padding(20)
                                .frame(width: cabalCardWidth, alignment: .leading)
                                .monacoSurfaceCard()
                                .contentShape(RoundedRectangle(cornerRadius: 24))
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("home-my-group-\(row.groupId)")
                        }
                    }
                }
                .contentMargins(.bottom, 2)
            }
        }
    }

    private func pnlChartSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            let headerLayout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6))
                : AnyLayout(HStackLayout())
            headerLayout {
                Text("Performance")
                    .font(.subheadline.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
                if !dynamicTypeSize.isAccessibilitySize { Spacer() }
                Text("LAST HOUR")
                    .font(.caption2.weight(.semibold))
                    .tracking(0.8)
            }
            .foregroundStyle(MonacoTheme.secondaryText)

            if dashboard.pnlSeries1H.count < 2 {
                Text("Your chart builds as you add money and trade.")
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .frame(maxWidth: .infinity, minHeight: 80, alignment: .leading)
            } else {
                Chart(dashboard.pnlSeries1H) { point in
                    AreaMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(MonacoTheme.primaryText.opacity(0.05))
                    LineMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                    .foregroundStyle(MonacoTheme.primaryText)
                }
                .chartYAxis {
                    AxisMarks(position: .leading, values: .automatic(desiredCount: 3))
                }
                .chartXAxis {
                    AxisMarks(values: .automatic(desiredCount: 3))
                }
                .frame(height: 150)
            }
        }
        .accessibilityIdentifier("home-pnl-chart")
    }

    private func leaderboardSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            sectionHeading("Leaderboard")

            Picker("Range", selection: $leaderboardRange) {
                ForEach(HomeLeaderboardRange.allCases, id: \.self) { range in
                    Text(range.label).tag(range)
                }
            }
            .pickerStyle(.segmented)
            .monacoSegmentedBoardPicker()

            if dashboard.leaderboard.people.isEmpty {
                Text("Returns appear here once cabals are funded.")
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(dashboard.leaderboard.people.enumerated()), id: \.element.userId) { index, row in
                        NavigationLink {
                            UserProfileGroupsView(
                                auth: auth,
                                userId: row.userId,
                                displayName: row.displayName
                            )
                        } label: {
                            leaderboardRow(row, rank: index + 1)
                                .padding(.vertical, 14)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("home-leaderboard-row-\(row.userId)")
                        if index < dashboard.leaderboard.people.count - 1 {
                            Divider().overlay(MonacoTheme.border)
                        }
                    }
                }
            }
        }
    }

    private func leaderboardRow(_ row: HomePeopleBoardRowDTO, rank: Int) -> some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
            : AnyLayout(HStackLayout(spacing: 12))
        return layout {
            HStack(spacing: 12) {
                Text(String(rank))
                    .font(.caption.weight(.semibold).monospacedDigit())
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .frame(minWidth: 16)
                MonacoIdentityMark(title: row.displayName, size: 38)
                Text(row.displayName)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(MonacoTheme.primaryText)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? nil : 2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !dynamicTypeSize.isAccessibilitySize { Spacer(minLength: 8) }
            boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
        }
    }

    private func missedProposalsSection(_ dashboard: HomeDashboardDTO) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            sectionHeading("Your vote is needed")

            if dashboard.missedProposals.isEmpty {
                Label("No proposals waiting for your vote.", systemImage: "checkmark.circle")
                    .font(.subheadline)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                ForEach(dashboard.missedProposals) { row in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        HStack(spacing: 14) {
                            MonacoIdentityMark(title: AssetSymbolFormatter.format(row.symbol), size: 44)
                            VStack(alignment: .leading, spacing: 5) {
                                Text(row.groupName)
                                    .font(.caption)
                                    .foregroundStyle(MonacoTheme.secondaryText)
                                Text(AssetSymbolFormatter.format(row.symbol))
                                    .font(MonacoTheme.display(21))
                                    .foregroundStyle(MonacoTheme.primaryText)
                            }
                            Spacer(minLength: 8)
                            Text("Vote")
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(MonacoTheme.surface)
                                .padding(.horizontal, 18)
                                .padding(.vertical, 11)
                                .background(MonacoTheme.primaryText, in: Capsule())
                        }
                        .padding(18)
                        .background(MonacoTheme.peach, in: RoundedRectangle(cornerRadius: 24))
                        .contentShape(RoundedRectangle(cornerRadius: 24))
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-missed-proposal-\(row.proposalId)")
                }
            }
        }
    }

    private func sectionHeading(_ title: String, detail: String? = nil) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title)
                .font(MonacoTheme.display(23))
                .accessibilityAddTraits(.isHeader)
                .foregroundStyle(MonacoTheme.primaryText)
            Spacer()
            if let detail {
                Text(detail)
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(MonacoTheme.secondaryText)
            }
        }
    }

    @ViewBuilder
    private func boardMetrics(percentReturn: String?, dollarPnl: String) -> some View {
        VStack(alignment: dynamicTypeSize.isAccessibilitySize ? .leading : .trailing, spacing: 2) {
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
            try await dashboardState.refresh {
                try await apiClient.getHomeDashboard(
                    accessToken: accessToken,
                    leaderboardRange: leaderboardRange
                )
            }
        } catch is CancellationError {
            return
        } catch let error as URLError where error.code == .cancelled {
            return
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            await auth.logout()
        } catch {
            errorMessage = "Could not load home dashboard."
            if dashboard != nil {
                refreshToast = MonacoToast(message: "Could not refresh Home. Pull down to retry.")
            }
        }

        isLoading = false
    }

}

#Preview {
    NavigationStack {
        HomeView(auth: PrivyAuthService()).monacoRootAppearance()
    }
}
