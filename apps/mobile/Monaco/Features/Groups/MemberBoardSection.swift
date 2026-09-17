import SwiftUI

struct MemberBoardSection: View {
    let members: [LeaderboardRowDTO]

    var body: some View {
        Section("Member board") {
            if members.isEmpty {
                Text("No members ranked yet.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
            } else {
                ForEach(members) { row in
                    HStack {
                        Text("#\(row.rank)")
                            .font(.caption.bold())
                            .foregroundStyle(MonacoTheme.secondaryText)
                            .frame(width: 28, alignment: .leading)
                        Text(row.displayName)
                            .font(.body.bold())
                            .foregroundStyle(MonacoTheme.primaryText)
                        Spacer()
                        VStack(alignment: .trailing, spacing: 2) {
                            if let percentReturn = row.percentReturn {
                                Text(percentReturn)
                                    .font(.subheadline.monospacedDigit())
                                    .foregroundStyle(MonacoTheme.primaryText)
                            } else {
                                Text("—")
                                    .foregroundStyle(MonacoTheme.secondaryText)
                            }
                            Text(row.dollarPnl)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(MonacoTheme.secondaryText)
                        }
                    }
                    .accessibilityIdentifier("member-board-row-\(row.rank)")
                }
            }
        }
    }
}
