import SwiftUI

/// Profile list of groups for a people-board row tap-through.
struct UserProfileGroupsView: View {
    let displayName: String
    let groups: [HomeGroupBoardRowDTO]

    var body: some View {
        List {
            Section("\(displayName)'s clubs") {
                if groups.isEmpty {
                    Text("No shared clubs yet.")
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(groups) { row in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.name)
                                .font(.body.bold())
                            Text(row.dollarPnl)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
    }
}
