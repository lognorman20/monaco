import MonacoCore
import SwiftUI

/// Cabals list — moved from Home segmented board (lane 148 owns richer Groups UX).
struct GroupsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    var onRefresh: () async -> Void = {}

    var body: some View {
        List {
            if home.groups.isEmpty {
                MonacoEmptyStateCard(
                    message: "No cabals yet. Create or join one to start investing together.",
                    systemImage: "person.3"
                )
            } else {
                ForEach(home.groups) { row in
                    NavigationLink {
                        if row.isJoined {
                            GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
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
                    .accessibilityIdentifier("groups-row-\(row.groupId)")
                }
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
        .refreshable {
            await onRefresh()
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
