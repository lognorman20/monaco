import SwiftUI

struct MemberBoardSection: View {
    let members: [LeaderboardRowDTO]

    var body: some View {
        Section("Member board") {
            if members.isEmpty {
                Text("No members ranked yet.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(members) { row in
                    HStack {
                        Text("#\(row.rank)")
                            .font(.caption.bold())
                            .foregroundStyle(.secondary)
                            .frame(width: 28, alignment: .leading)
                        Text(row.displayName)
                            .font(.body.bold())
                        Spacer()
                        VStack(alignment: .trailing, spacing: 2) {
                            if let percentReturn = row.percentReturn {
                                Text(percentReturn)
                                    .font(.subheadline.monospacedDigit())
                            } else {
                                Text("—")
                                    .foregroundStyle(.secondary)
                            }
                            Text(row.dollarPnl)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityIdentifier("member-board-row-\(row.rank)")
                }
            }
        }
    }
}
