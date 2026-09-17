import SwiftUI

/// Profile list of groups for a people-board row tap-through.
struct UserProfileGroupsView: View {
    let displayName: String
    let groups: [HomeGroupBoardRowDTO]

    var body: some View {
        List {
            Section("\(displayName)'s clubs") {
                if groups.isEmpty {
                    MonacoEmptyStateCard(
                        message: "No shared clubs yet.",
                        systemImage: "person.3"
                    )
                } else {
                    ForEach(groups) { row in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.name)
                                .font(.body.bold())
                                .foregroundStyle(MonacoTheme.primaryText)
                            Text(row.dollarPnl)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(MonacoTheme.secondaryText)
                        }
                    }
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
    }
}
