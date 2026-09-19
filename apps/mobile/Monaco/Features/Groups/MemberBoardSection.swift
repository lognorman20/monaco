import MonacoCore
import SwiftUI

/// Leaderboard: every member ranked by return. The leader's row gets a thin ink outline.
struct MemberBoardSection: View {
    let members: [LeaderboardRowDTO]
    var currentUserId: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Leaderboard")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if members.isEmpty {
                Text("No members yet.")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.muted)
            } else {
                VStack(spacing: 0) {
                    ForEach(members) { row in
                        memberRow(row)
                    }
                }
                .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
                .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
            }
        }
        .accessibilityIdentifier("group-leaderboard")
    }

    private func memberRow(_ row: LeaderboardRowDTO) -> some View {
        HStack(spacing: 12) {
            Text("\(row.rank)")
                .font(.body.weight(.semibold).monospacedDigit())
                .foregroundStyle(MonacoTheme.muted)
                .frame(width: 24, alignment: .leading)
            MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 36)
            Text(displayName(row))
                .font(.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                Text(PercentReturnFormatter.format(row.percentReturn))
                    .font(.body.weight(.semibold).monospacedDigit())
                    .foregroundStyle(MonacoTheme.signed(row.percentReturn))
                Text(row.dollarPnl)
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(MonacoTheme.signed(row.dollarPnl))
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("member-board-row-\(row.rank)")
    }

    private func displayName(_ row: LeaderboardRowDTO) -> String {
        guard let currentUserId, row.userId == currentUserId else { return row.displayName }
        return "\(row.displayName) (you)"
    }
}
