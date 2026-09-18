import MonacoCore
import SwiftUI

/// Groups tab: P&L chart, my cabals strip, search, platform leaderboard.
struct GroupsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @State private var searchText = ""
    @State private var searchResults: [GroupDiscoveryRow] = []
    @State private var leaderboard: [GroupLeaderboardRow] = []
    @State private var chartSeries: [GroupPnLChartSeries] = []
    @State private var isLoadingTabData = false
    @State private var searchTask: Task<Void, Never>?

    private var myGroups: [HomeGroupBoardRowDTO] {
        home.groups.filter(\.isJoined)
    }

    var body: some View {
        List {
            Section {
                GroupsPnLChartView(series: chartSeries)
            } header: {
                Text("P&L over time")
            }

            if !myGroups.isEmpty {
                Section {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 12) {
                            ForEach(myGroups) { row in
                                NavigationLink {
                                    GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                                } label: {
                                    myGroupCard(row)
                                }
                                .buttonStyle(.plain)
                                .accessibilityIdentifier("my-group-card-\(row.groupId)")
                            }
                        }
                        .padding(.vertical, 4)
                    }
                } header: {
                    Text("My cabals")
                }
            }

            Section {
                TextField("Search cabals by name", text: $searchText)
                    .textInputAutocapitalization(.words)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("groups-search-field")
                    .onChange(of: searchText) { _, newValue in
                        scheduleSearch(for: newValue)
                    }

                if searchText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Text("Type to discover cabals across Monaco.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                } else if searchResults.isEmpty {
                    Text("No cabals match that name.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                } else {
                    ForEach(searchResults) { row in
                        NavigationLink {
                            if row.isJoined {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                            } else {
                                JoinGroupView(auth: auth, groupId: row.groupId)
                            }
                        } label: {
                            searchResultRow(row)
                        }
                        .accessibilityIdentifier("groups-search-row-\(row.groupId)")
                    }
                }
            } header: {
                Text("Search")
            }

            Section {
                if leaderboard.isEmpty {
                    Text("Funded cabals with deposits rank here by percent return.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                } else {
                    ForEach(leaderboard) { row in
                        NavigationLink {
                            if row.isJoined {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                            } else {
                                JoinGroupView(auth: auth, groupId: row.groupId)
                            }
                        } label: {
                            leaderboardRow(row)
                        }
                        .accessibilityIdentifier("groups-leaderboard-row-\(row.groupId)")
                    }
                }
            } header: {
                Text("All cabals")
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Groups")
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
                .accessibilityIdentifier("groups-club-menu")
            }
        }
        .overlay {
            if isLoadingTabData {
                ProgressView()
                    .tint(MonacoTheme.accent)
            }
        }
        .task(id: home.groups.map(\.groupId)) {
            await loadTabData()
        }
        .refreshable {
            await onRefresh()
            await loadTabData()
        }
    }

    @ViewBuilder
    private func myGroupCard(_ row: HomeGroupBoardRowDTO) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(row.name)
                .font(.subheadline.bold())
                .foregroundStyle(MonacoTheme.primaryText)
                .lineLimit(2)
                .multilineTextAlignment(.leading)
            Text("$\(row.potValueUsd)")
                .font(.caption.monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
            Text(row.dollarPnl)
                .font(.caption.monospacedDigit())
                .foregroundStyle(pnlColor(for: row.dollarPnl))
        }
        .frame(width: 140, alignment: .leading)
        .padding(12)
        .monacoSurfaceCard()
    }

    @ViewBuilder
    private func searchResultRow(_ row: GroupDiscoveryRow) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.name)
                    .font(.body.bold())
                    .foregroundStyle(MonacoTheme.primaryText)
                if !row.isJoined {
                    Text(row.joinMode == "password" ? "Password required" : "Open cabal")
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            Spacer()
            if !row.isJoined {
                Text("Join")
                    .font(.caption.bold())
                    .foregroundStyle(MonacoTheme.accent)
            }
            groupBoardMetrics(
                potValueUsd: row.potValueUsd,
                percentReturn: row.percentReturn,
                dollarPnl: row.dollarPnl
            )
        }
    }

    @ViewBuilder
    private func leaderboardRow(_ row: GroupLeaderboardRow) -> some View {
        HStack(spacing: 12) {
            Text("#\(row.rank)")
                .font(.caption.bold().monospacedDigit())
                .foregroundStyle(MonacoTheme.secondaryText)
                .frame(width: 28, alignment: .leading)
            Text(row.name)
                .font(.body.bold())
                .foregroundStyle(MonacoTheme.primaryText)
            Spacer()
            groupBoardMetrics(
                potValueUsd: row.potValueUsd,
                percentReturn: row.percentReturn,
                dollarPnl: row.dollarPnl
            )
        }
    }

    @ViewBuilder
    private func groupBoardMetrics(potValueUsd: String, percentReturn: String?, dollarPnl: String) -> some View {
        VStack(alignment: .trailing, spacing: 2) {
            Text("$\(potValueUsd)")
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
            Text(PercentReturnFormatter.format(percentReturn))
                .font(.caption.monospacedDigit())
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

    private func scheduleSearch(for query: String) {
        searchTask?.cancel()
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmed.count >= 2 else {
            searchResults = []
            return
        }
        searchTask = Task {
            try? await Task.sleep(nanoseconds: 300_000_000)
            guard !Task.isCancelled else { return }
            await runSearch(query: trimmed)
        }
    }

    @MainActor
    private func runSearch(query: String) async {
        guard let accessToken = auth.accessToken else { return }
        do {
            let response = try await apiClient.searchGroups(accessToken: accessToken, query: query)
            searchResults = response.groups
        } catch {
            searchResults = []
        }
    }

    @MainActor
    private func loadTabData() async {
        guard let accessToken = auth.accessToken else { return }
        isLoadingTabData = true
        defer { isLoadingTabData = false }

        async let leaderboardTask: Void = {
            do {
                let response = try await apiClient.getGroupLeaderboard(accessToken: accessToken)
                leaderboard = response.groups
            } catch {
                leaderboard = []
            }
        }()

        async let chartTask: Void = {
            var loaded: [GroupPnLChartSeries] = []
            for group in myGroups {
                do {
                    let history = try await apiClient.getGroupPnLHistory(accessToken: accessToken, groupId: group.groupId)
                    let points = history.points.compactMap { point -> GroupPnLChartPoint? in
                        guard let date = ISO8601DateFormatter().date(from: point.at),
                              let value = Double(point.potValueUsd) else {
                            return nil
                        }
                        return GroupPnLChartPoint(date: date, potValueUsd: value, groupId: group.groupId)
                    }
                    loaded.append(GroupPnLChartSeries(groupId: group.groupId, name: group.name, points: points))
                } catch {
                    continue
                }
            }
            chartSeries = loaded
        }()

        _ = await (leaderboardTask, chartTask)
    }
}

#Preview {
    NavigationStack {
        GroupsTabView(
            auth: PrivyAuthService(),
            home: HomeViewDTO(
                groups: [
                    HomeGroupBoardRowDTO(
                        groupId: "g1",
                        name: "Weekend investors",
                        potValueUsd: "548.20",
                        percentReturn: "0.124",
                        dollarPnl: "+48.20",
                        isJoined: true
                    ),
                ],
                people: []
            )
        )
        .monacoRootAppearance()
    }
}
