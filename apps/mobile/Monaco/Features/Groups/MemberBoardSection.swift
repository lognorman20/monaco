import MonacoCore
import SwiftUI

/// Leaderboard: every member ranked by return. The leader wears the crown; the viewer's own
/// row is washed so it can be found in a long board.
struct MemberBoardSection: View {
    let members: [LeaderboardRowDTO]
    var currentUserId: String?

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Leaderboard")
                .padding(.horizontal, MonacoTheme.Space.m)

            if members.isEmpty {
                Text("No members yet.")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .padding(.horizontal, MonacoTheme.Space.m)
            } else {
                MonacoGroupedList {
                    ForEach(members) { row in
                        BoardRow(
                            rank: row.rank,
                            name: row.displayName,
                            detail: isViewer(row) ? "You" : nil,
                            percentReturn: row.percentReturn,
                            dollarPnl: row.dollarPnl,
                            isViewer: isViewer(row),
                            isLast: row.id == members.last?.id
                        ) {
                            MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 40)
                        }
                        .accessibilityIdentifier("member-board-row-\(row.rank)")
                    }
                }
            }
        }
        .accessibilityIdentifier("group-leaderboard")
    }

    private func isViewer(_ row: LeaderboardRowDTO) -> Bool {
        guard let currentUserId else { return false }
        return row.userId == currentUserId
    }
}
