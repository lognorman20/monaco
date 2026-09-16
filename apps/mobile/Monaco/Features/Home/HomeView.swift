import SwiftUI

/// App home scaffold — group and people boards filled in M5-T14/T15.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    @State private var selectedTab = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Home")
                .font(.title.bold())

            Picker("Board", selection: $selectedTab) {
                Text("Groups").tag(0)
                Text("People").tag(1)
            }
            .pickerStyle(.segmented)

            if selectedTab == 0 {
                groupBoardSection
            } else {
                peopleBoardSection
            }

            Button("Sign out") {
                Task { await auth.logout() }
            }
            .buttonStyle(.bordered)
        }
        .refreshable {
            await onRefresh()
        }
    }

    private var groupBoardSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            if home.groups.isEmpty {
                Text("No groups yet. Create or join one to start investing together.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(home.groups) { row in
                    HStack {
                        Text(row.name)
                            .font(.body.bold())
                        Spacer()
                        boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
                    }
                }
            }
        }
    }

    private var peopleBoardSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            if home.people.isEmpty {
                Text("No leaderboard rows yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(home.people) { row in
                    HStack {
                        Text(row.displayName)
                            .font(.body.bold())
                        Spacer()
                        boardMetrics(percentReturn: row.percentReturn, dollarPnl: row.dollarPnl)
                    }
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
