import MonacoCore
import SwiftUI

/// Every funded cabal on Monaco, ranked by percent return.
struct CabalsLeaderboardSection: View {
    let model: CabalsTabModel
    var onSelect: (CabalsRoute) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Top cabals")
            Text("Ranked by return across everyone on Monaco")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)

            if model.isLeaderboardLoading {
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .accessibilityIdentifier("cabals-leaderboard-loading")
            } else if model.leaderboardFailed, model.leaderboard.isEmpty {
                VStack(spacing: MonacoTheme.Space.s) {
                    Text("Couldn't load the board.")
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.muted)
                    Button("Try again") {
                        Task { await model.loadLeaderboard() }
                    }
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("cabals-leaderboard-retry")
                }
                .frame(maxWidth: .infinity)
                .monacoSurfaceCard()
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
                            onSelect(CabalsRoute(row: row.groupID, name: row.name, isJoined: row.isJoined, joinMode: row.joinMode))
                        } label: {
                            CabalDiscoveryRowContent(
                                rank: row.rank,
                                groupId: row.groupID,
                                name: row.name,
                                pictureUrl: row.pictureUrl,
                                detail: cabalRowDetail(memberCount: row.memberCount, isJoined: row.isJoined, joinMode: row.joinMode),
                                potValueUsd: row.potValueUsd,
                                percentReturn: row.percentReturn,
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

/// Shared row content for the board and search results: rank (when known) on
/// the cabal's tinted mark, name / member summary, percent over pot value.
struct CabalDiscoveryRowContent: View {
    let rank: Int?
    let groupId: String
    let name: String
    /// The cabal's picture; nil draws its tinted initials.
    var pictureUrl: String? = nil
    let detail: String
    let potValueUsd: String
    let percentReturn: String?
    let isLast: Bool

    var body: some View {
        MonacoRow(
            title: name,
            subtitle: detail,
            isLast: isLast,
            leading: {
                if let rank {
                    RankedCabalMark(rank: rank, groupId: groupId, name: name, pictureUrl: pictureUrl)
                } else {
                    CabalMark(groupId: groupId, name: name, size: 40, pictureUrl: pictureUrl)
                }
            },
            trailing: {
                PercentText(percentReturn: percentReturn, style: .row)
                MoneyText(decimalString: potValueUsd, style: .caption, color: MonacoTheme.muted)
            }
        )
    }
}

/// A cabal's mark with its platform rank badged at the corner. The mark itself
/// is decoration, but the rank is the whole point of this board, so VoiceOver
/// reads it as the first thing in the row.
private struct RankedCabalMark: View {
    let rank: Int
    let groupId: String
    let name: String
    var pictureUrl: String? = nil

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            CabalMark(groupId: groupId, name: name, size: 40, pictureUrl: pictureUrl)
                .accessibilityHidden(true)
            Text("\(rank)")
                .font(.system(size: 10, weight: .bold))
                .foregroundStyle(MonacoTheme.primaryButtonLabel)
                .frame(minWidth: 16, minHeight: 16)
                .background(Circle().fill(MonacoTheme.ink))
                .offset(x: 4, y: 4)
                .accessibilityLabel("Rank \(rank)")
        }
    }
}
