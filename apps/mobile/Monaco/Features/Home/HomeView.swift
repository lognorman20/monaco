import MonacoCore
import SwiftUI

/// App home with group board and people board tabs.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    @State private var selectedTab = 0

    var body: some View {
        VStack(spacing: 0) {
            Picker("Board", selection: $selectedTab) {
                Text("Groups").tag(0)
                Text("People").tag(1)
            }
            .pickerStyle(.segmented)
            .monacoSegmentedBoardPicker()

            if selectedTab == 0 {
                groupBoardSection
            } else {
                peopleBoardSection
            }
        }
        .background(MonacoTheme.background)
        .navigationTitle("Home")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink {
                    SettingsView(auth: auth, memberWalletAddress: nil, treasuryAddress: nil)
                } label: {
                    Image(systemName: "gearshape")
                        .monacoToolbarIcon()
                }
                .accessibilityIdentifier("home-settings-link")
            }
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    NavigationLink {
                        CreateGroupView(auth: auth)
                    } label: {
                        Label("Create club", systemImage: "plus")
                    }
                    NavigationLink {
                        JoinGroupView(auth: auth)
                    } label: {
                        Label("Join club", systemImage: "person.badge.plus")
                    }
                } label: {
                    Image(systemName: "plus.circle")
                        .monacoToolbarIcon()
                }
                .accessibilityIdentifier("home-club-menu")
            }
        }
        .refreshable {
            await onRefresh()
        }
    }

    private var groupBoardSection: some View {
        List {
            if home.groups.isEmpty {
                MonacoEmptyStateCard(
                    message: "No clubs yet. Create or join one to start investing together.",
                    systemImage: "person.3"
                )
            } else {
                ForEach(home.groups) { row in
                    NavigationLink {
                        if row.isJoined {
                            GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name)
                        } else {
                            JoinGroupView(auth: auth, groupId: row.groupId)
                        }
                    } label: {
                        HStack {
                            Text(row.name)
                                .font(.body.bold())
                                .foregroundStyle(MonacoTheme.primaryText)
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
                    .accessibilityIdentifier("home-group-row-\(row.groupId)")
                }
            }
        }
        .monacoInsetList()
    }

    private var peopleBoardSection: some View {
        List {
            if home.people.isEmpty {
                MonacoEmptyStateCard(
                    message: peopleEmptyMessage,
                    systemImage: "chart.bar"
                )
            } else {
                ForEach(home.people) { row in
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
                    }
                    .accessibilityIdentifier("home-people-row-\(row.userId)")
                }
            }
        }
        .monacoInsetList()
    }

    private var peopleEmptyMessage: String {
        if home.groups.isEmpty {
            return "Join a club to see members on the leaderboard."
        }
        return "No members in your clubs yet."
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
}

#Preview {
    NavigationStack {
        HomeView(
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
                people: [
                    HomePeopleBoardRowDTO(
                        userId: "u1",
                        displayName: "Alfred",
                        percentReturn: "0.124",
                        dollarPnl: "+48.20"
                    ),
                ]
            )
        )
        .monacoRootAppearance()
    }
}
