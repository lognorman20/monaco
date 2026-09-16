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
            .padding()

            if selectedTab == 0 {
                groupBoardSection
            } else {
                peopleBoardSection
            }
        }
        .navigationTitle("Home")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                NavigationLink {
                    SettingsView(auth: auth, memberWalletAddress: nil, treasuryAddress: nil)
                } label: {
                    Image(systemName: "gearshape")
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
                Text("No clubs yet. Create or join one to start investing together.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(home.groups) { row in
                    NavigationLink {
                        GroupDetailView(auth: auth, groupId: row.groupId)
                    } label: {
                        HStack {
                            Text(row.name)
                                .font(.body.bold())
                            Spacer()
                            boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
                        }
                    }
                    .accessibilityIdentifier("home-group-row-\(row.groupId)")
                }
            }
        }
    }

    private var peopleBoardSection: some View {
        List {
            if home.people.isEmpty {
                Text("No leaderboard rows yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(home.people) { row in
                    NavigationLink {
                        UserProfileGroupsView(displayName: row.displayName, groups: home.groups)
                    } label: {
                        HStack {
                            Text(row.displayName)
                                .font(.body.bold())
                            Spacer()
                            boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
                        }
                    }
                    .accessibilityIdentifier("home-people-row-\(row.userId)")
                }
            }
        }
    }

    @ViewBuilder
    private func boardMetrics(percentReturn: String?, dollarPnl: String) -> some View {
        VStack(alignment: .trailing, spacing: 2) {
            if let percentReturn {
                Text(percentReturn)
                    .font(.subheadline.monospacedDigit())
            } else {
                Text("—")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Text(dollarPnl)
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
        }
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
                        percentReturn: "+12.4%",
                        dollarPnl: "+$48.20"
                    ),
                ],
                people: [
                    HomePeopleBoardRowDTO(
                        userId: "u1",
                        displayName: "Alfred",
                        percentReturn: "+12.4%",
                        dollarPnl: "+$48.20"
                    ),
                ]
            )
        )
    }
}
