import SwiftUI

/// Portfolio placeholder — lane 156 owns catalog/search and full holdings UX.
struct AssetsView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    private var joinedGroups: [HomeGroupBoardRowDTO] {
        home.groups.filter(\.isJoined)
    }

    var body: some View {
        List {
            if joinedGroups.isEmpty {
                MonacoEmptyStateCard(
                    message: "Join a cabal and add money to see your holdings here.",
                    systemImage: "chart.pie"
                )
            } else {
                Section {
                    ForEach(joinedGroups) { row in
                        NavigationLink {
                            GroupDetailView(
                                auth: auth,
                                groupId: row.groupId,
                                groupName: row.name,
                                onLeft: onRefresh
                            )
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(row.name)
                                        .font(.body.bold())
                                        .foregroundStyle(MonacoTheme.primaryText)
                                    Text("Cabal treasury")
                                        .font(.caption)
                                        .foregroundStyle(MonacoTheme.secondaryText)
                                }
                                Spacer()
                                Text("$\(row.potValueUsd)")
                                    .font(.subheadline.monospacedDigit())
                                    .foregroundStyle(MonacoTheme.primaryText)
                            }
                        }
                        .accessibilityIdentifier("assets-group-row-\(row.groupId)")
                    }
                } header: {
                    Text("By cabal")
                } footer: {
                    Text("Full portfolio view coming soon. Open a cabal for holdings detail.")
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Assets")
        .refreshable {
            await onRefresh()
        }
    }
}

#Preview {
    NavigationStack {
        AssetsView(
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
