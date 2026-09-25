import MonacoCore
import SwiftUI

/// Every funded cabal on Monaco, ranked by percent return. The leader wears the crown.
struct CabalsLeaderboardSection: View {
    let model: CabalsTabModel
    var onSelect: (CabalsRoute) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            VStack(alignment: .leading, spacing: 2) {
                MonacoSectionHeader("Top cabals")
                Text("Ranked by return across everyone on Monaco")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            .padding(.horizontal, MonacoTheme.Space.m)

            if model.isLeaderboardLoading {
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .accessibilityIdentifier("cabals-leaderboard-loading")
            } else if model.leaderboardFailed, model.leaderboard.isEmpty {
                EmptyState(
                    title: "Couldn't load the board",
                    actionTitle: "Try again",
                    action: { Task { await model.loadLeaderboard() } }
                )
                .accessibilityIdentifier("cabals-leaderboard-error")
            } else if model.leaderboard.isEmpty {
                EmptyState(
                    title: "No cabal has put money in yet",
                    message: "The first one to fund takes the top spot."
                )
                .accessibilityIdentifier("cabals-leaderboard-empty")
            } else {
                MonacoGroupedList {
                    ForEach(Array(model.leaderboard.enumerated()), id: \.element.id) { index, row in
                        Button {
                            onSelect(CabalsRoute(
                                row: row.groupID, name: row.name, isJoined: row.isJoined, joinMode: row.joinMode,
                                memberCount: row.memberCount, pictureUrl: row.pictureUrl
                            ))
                        } label: {
                            CabalDiscoveryRowContent(
                                rank: row.rank,
                                groupId: row.groupID,
                                name: row.name,
                                pictureUrl: row.pictureUrl,
                                detail: cabalRowDetail(memberCount: row.memberCount, isJoined: row.isJoined, joinMode: row.joinMode),
                                potValueUsd: row.potValueUsd,
                                percentReturn: row.percentReturn,
                                isViewer: row.isJoined,
                                isLast: index == model.leaderboard.count - 1
                            )
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("cabals-leaderboard-row-\(row.groupID)")
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("cabals-leaderboard")
            }
        }
    }
}

func cabalRowDetail(memberCount: Int, isJoined: Bool, joinMode: GroupJoinMode) -> String {
    let members = memberCount == 1 ? "1 member" : "\(memberCount) members"
    if isJoined { return "\(members) · You're in" }
    switch joinMode {
    case .open: return "\(members) · Open"
    case .request: return "\(members) · Ask to join"
    }
}

/// Shared row content for the board and search results: the rank (when the list is ranked),
/// the cabal's mark, name over member summary, percent over pot value.
struct CabalDiscoveryRowContent: View {
    let rank: Int?
    let groupId: String
    let name: String
    /// The cabal's picture; nil draws its tinted initials.
    var pictureUrl: String? = nil
    let detail: String
    let potValueUsd: String
    let percentReturn: String?
    /// A cabal the viewer belongs to is washed, so their own show up in a long board.
    var isViewer = false
    let isLast: Bool

    var body: some View {
        BoardRow(
            rank: rank,
            name: name,
            detail: detail,
            percentReturn: percentReturn,
            potValueUsd: potValueUsd,
            isViewer: isViewer,
            isLast: isLast,
            chevron: true
        ) {
            CabalMark(groupId: groupId, name: name, size: 40, pictureUrl: pictureUrl)
        }
    }
}
