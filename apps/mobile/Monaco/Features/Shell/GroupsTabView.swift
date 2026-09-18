import MonacoCore
import SwiftUI

/// Groups tab: P&L chart, my cabals strip, search, platform leaderboard.
struct GroupsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    @Environment(\.monacoSessionRevision) private var sessionRevision
    var onRefresh: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @State private var searchText = ""
    @State private var searchResults: [GroupDiscoveryRow] = []
    @State private var leaderboard: [GroupLeaderboardRow] = []
    @State private var chartSeries: [GroupPnLChartSeries] = []
    @State private var isLoadingTabData = false
    @State private var loadID = UUID()
    @State private var refreshToast: MonacoToast?
    @State private var searchTask: Task<Void, Never>?

    private var myGroups: [HomeGroupBoardRowDTO] {
        home.groups.filter(\.isJoined)
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 28) {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Invest together.")
                        .font(MonacoTheme.display(32))
                    Text("Your people. A shared portfolio.")
                        .font(.subheadline)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .padding(.top, 8)

                if !myGroups.isEmpty {
                    sectionHeading("Your cabals", detail: "\(myGroups.count) joined")
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 12) {
                            ForEach(myGroups) { row in
                                NavigationLink {
                                    GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                                } label: { myGroupCard(row) }
                                .buttonStyle(.plain)
                                .accessibilityIdentifier("my-group-card-\(row.groupId)")
                            }
                        }
                    }
                    .contentMargins(.trailing, 4)
                }

                VStack(alignment: .leading, spacing: 18) {
                    sectionHeading("Cabal performance", detail: "Pot value")
                    GroupsPnLChartView(series: chartSeries)
                }
                .padding(20)
                .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 24))

                VStack(alignment: .leading, spacing: 16) {
                    sectionHeading("Find your people", detail: "")
                    HStack(spacing: 12) {
                        Image(systemName: "magnifyingglass").foregroundStyle(MonacoTheme.secondaryText)
                        TextField("Search cabals by name", text: $searchText)
                            .textInputAutocapitalization(.words)
                            .autocorrectionDisabled()
                            .accessibilityIdentifier("groups-search-field")
                            .onChange(of: searchText) { _, value in scheduleSearch(for: value) }
                    }
                    .padding(16)
                    .background(MonacoTheme.surface, in: Capsule())
                    .overlay(Capsule().strokeBorder(MonacoTheme.border, lineWidth: 1))

                    if !searchText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        if searchResults.isEmpty {
                            Text("No cabals match that name.")
                                .font(.subheadline).foregroundStyle(MonacoTheme.secondaryText)
                        }
                        ForEach(searchResults) { row in
                            NavigationLink {
                                if isJoined(row.groupId) {
                                    GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                                } else { JoinGroupView(auth: auth, groupId: row.groupId) }
                            } label: { searchResultRow(row).padding(.vertical, 8).contentShape(Rectangle()) }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("groups-search-row-\(row.groupId)")
                        }
                    }
                }

                VStack(alignment: .leading, spacing: 16) {
                    sectionHeading("The leaderboard", detail: "All cabals")
                    if leaderboard.isEmpty {
                        Text("Funded cabals rank here by their return.")
                            .font(.subheadline).foregroundStyle(MonacoTheme.secondaryText)
                    }
                    ForEach(leaderboard) { row in
                        NavigationLink {
                            if isJoined(row.groupId) {
                                GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                            } else { JoinGroupView(auth: auth, groupId: row.groupId) }
                        } label: { leaderboardRow(row).padding(.vertical, 10).contentShape(Rectangle()) }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("groups-leaderboard-row-\(row.groupId)")
                    }
                }
            }
            .padding(20)
            .padding(.bottom, 16)
        }
        .foregroundStyle(MonacoTheme.primaryText)
        .background(MonacoTheme.background)
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($refreshToast)
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
        .task(id: sessionRevision) {
            await loadTabData()
        }
        .onDisappear { searchTask?.cancel() }
        .refreshable {
            await onRefresh()
        }
    }

    private func isJoined(_ groupId: String) -> Bool {
        myGroups.contains { $0.groupId == groupId }
    }

    private func sectionHeading(_ title: String, detail: String) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(MonacoTheme.display(20)).accessibilityAddTraits(.isHeader)
            Spacer()
            if !detail.isEmpty { Text(detail).font(.caption).foregroundStyle(MonacoTheme.secondaryText) }
        }
    }

    private func myGroupCard(_ row: HomeGroupBoardRowDTO) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                MonacoIdentityMark(title: row.name, size: 48)
                Spacer()
                Image(systemName: "arrow.up.right").font(.subheadline.weight(.semibold))
            }
            Text(row.name).font(MonacoTheme.display(20)).lineLimit(2)
                .frame(minHeight: 52, alignment: .topLeading)
            VStack(alignment: .leading, spacing: 4) {
                Text("$\(row.potValueUsd)").font(.title3.weight(.semibold).monospacedDigit())
                Text("\(row.dollarPnl) P&L").font(.caption.monospacedDigit()).foregroundStyle(pnlColor(for: row.dollarPnl))
            }
        }
        .padding(20)
        .frame(width: 230, alignment: .leading)
        .background(MonacoTheme.mint.opacity(0.65), in: RoundedRectangle(cornerRadius: 26))
        .foregroundStyle(MonacoTheme.primaryText)
    }

    @ViewBuilder
    private func searchResultRow(_ row: GroupDiscoveryRow) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.name)
                    .font(.body.bold())
                    .foregroundStyle(MonacoTheme.primaryText)
                if !isJoined(row.groupId) {
                    Text(row.joinMode == "request" ? "Admin approval required" : "Open cabal")
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            Spacer()
            if !isJoined(row.groupId) {
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
            guard !Task.isCancelled, searchText.trimmingCharacters(in: .whitespacesAndNewlines) == query else { return }
            searchResults = response.groups
        } catch {
            guard !Task.isCancelled else { return }
            searchResults = []
        }
    }

    @MainActor
    private func loadTabData() async {
        guard let accessToken = auth.accessToken else { return }
        let requestID = UUID()
        loadID = requestID
        isLoadingTabData = leaderboard.isEmpty && chartSeries.isEmpty
        defer { if loadID == requestID { isLoadingTabData = false } }

        async let leaderboardTask: Void = {
            do {
                let response = try await apiClient.getGroupLeaderboard(accessToken: accessToken)
                guard !Task.isCancelled, loadID == requestID else { return }
                leaderboard = response.groups
            } catch {
                guard !Task.isCancelled, loadID == requestID else { return }
                refreshToast = MonacoToast(message: "Could not refresh cabal rankings. Pull down to retry.")
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
                    guard !Task.isCancelled, loadID == requestID else { return }
                    if let previous = chartSeries.first(where: { $0.groupId == group.groupId }) {
                        loaded.append(previous)
                    }
                    refreshToast = MonacoToast(message: "Could not refresh cabal history. Pull down to retry.")
                }
            }
            guard !Task.isCancelled, loadID == requestID else { return }
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
