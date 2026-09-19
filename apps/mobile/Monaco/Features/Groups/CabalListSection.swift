import SwiftUI

/// Shared cabal rows for Home and the Cabals tab.
struct CabalListSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeGroupBoardRowDTO]
    var onRefresh: () async -> Void
    var emptyMessage: String
    var rowPrefix: String = "home-group-row"

    var body: some View {
        List {
            if rows.isEmpty {
                MonacoEmptyStateCard(
                    message: emptyMessage,
                    systemImage: "person.3"
                )
            } else {
                ForEach(rows) { row in
                    NavigationLink {
                        if row.isJoined {
                            GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name, onLeft: onRefresh)
                        } else {
                            JoinGroupView(auth: auth, groupId: row.groupId)
                        }
                    } label: {
                        MonacoRowCard(
                            systemImage: row.isJoined ? "person.3.fill" : "person.badge.plus",
                            title: row.name,
                            subtitle: row.isJoined ? nil : "Join",
                            trailing: "$\(row.potValueUsd)"
                        )
                        .accessibilityIdentifier("\(rowPrefix)-\(row.groupId)")
                    }
                    .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .accessibilityIdentifier("\(rowPrefix)-\(row.groupId)")
                }
            }
        }
        .monacoInsetList()
    }
}
