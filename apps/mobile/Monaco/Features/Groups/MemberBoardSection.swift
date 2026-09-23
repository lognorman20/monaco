import MonacoCore
import SwiftUI

/// Leaderboard: every member ranked by return. The leader's row gets a thin ink outline.
struct MemberBoardSection: View {
    let members: [LeaderboardRowDTO]
    var currentUserId: String?

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            MonacoSectionHeader("Leaderboard")

            if members.isEmpty {
                Text("No members yet.")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
            } else {
                MonacoGroupedList {
                    ForEach(members) { row in
                        memberRow(row, isLast: row.id == members.last?.id)
                    }
                }
            }
        }
        .accessibilityIdentifier("group-leaderboard")
    }

    private func memberRow(_ row: LeaderboardRowDTO, isLast: Bool) -> some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            Text("\(row.rank)")
                .font(MonacoTheme.Typo.moneyRow)
                .foregroundStyle(MonacoTheme.fgMuted)
                .frame(width: 24, alignment: .leading)
            MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 36)
            HStack(spacing: 4) {
                Text(row.displayName)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .lineLimit(1)
                if isViewer(row) {
                    Text("(you)")
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.fgMuted)
                        .fixedSize()
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            VStack(alignment: .trailing, spacing: 2) {
                PercentText(percentReturn: row.percentReturn, style: .row)
                PnLText(dollarPnl: row.dollarPnl, style: .caption)
            }
            .fixedSize()
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .overlay {
            if row.rank == 1 {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.container - 4, style: .continuous)
                    .strokeBorder(MonacoTheme.fgPrimary, lineWidth: 1)
                    .padding(4)
            }
        }
        .overlay(alignment: .bottom) {
            if !isLast, row.rank != 1 {
                Rectangle().fill(MonacoTheme.line).frame(height: 1).padding(.leading, 72)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("member-board-row-\(row.rank)")
    }

    private func isViewer(_ row: LeaderboardRowDTO) -> Bool {
        guard let currentUserId else { return false }
        return row.userId == currentUserId
    }
}
