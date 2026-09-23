import MonacoCore
import SwiftUI

/// Every funded cabal on Monaco, ranked by percent return.
struct CabalsLeaderboardSection: View {
    let model: CabalsTabModel
    /// Resolved across the viewer's own cabals; a cabal they are not in falls back to the hash.
    var tints: [String: MonacoTheme.CabalTint] = [:]
    var onSelect: (CabalsRoute) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            VStack(alignment: .leading, spacing: 2) {
                MonacoSectionHeader("Top cabals")
                Text("Ranked by return across everyone on Monaco")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
            }

            if model.isLeaderboardLoading {
                ProgressView()
                    .tint(MonacoTheme.controlTint)
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .accessibilityIdentifier("cabals-leaderboard-loading")
            } else if model.leaderboardFailed, model.leaderboard.isEmpty {
                VStack(spacing: MonacoTheme.Space.sm) {
                    Text("Couldn't load the board.")
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.fgMuted)
                    Button("Try again") {
                        Task { await model.loadLeaderboard() }
                    }
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("cabals-leaderboard-retry")
                }
                .frame(maxWidth: .infinity)
                .padding(MonacoTheme.Space.m)
                .monacoElevation(.card)
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
                                tint: CabalTintAssignment.tint(forGroupId: row.groupID, in: tints),
                                name: row.name,
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
                // No elevation here: `MonacoGroupedList` owns its own surface and radius, and
                // Chunk B moves it onto E1 in one place rather than each caller re-wrapping it
                // at a radius that does not match the one it clips to.
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
    /// Passed where the caller has resolved it; otherwise the mark hashes the id itself.
    var tint: MonacoTheme.CabalTint?
    let name: String
    let detail: String
    let potValueUsd: String
    let percentReturn: String?
    let isLast: Bool

    private var resolvedTint: MonacoTheme.CabalTint {
        tint ?? .forGroupId(groupId)
    }

    var body: some View {
        MonacoRow(
            title: name,
            subtitle: detail,
            isLast: isLast,
            leading: {
                if let rank {
                    RankedCabalMark(rank: rank, tint: resolvedTint, name: name)
                } else {
                    CabalMark(tint: resolvedTint, name: name, size: 40)
                }
            },
            trailing: {
                PercentText(percentReturn: percentReturn, style: .row)
                MoneyText(decimalString: potValueUsd, style: .caption, color: MonacoTheme.fgMuted)
            }
        )
    }
}

/// A cabal's mark with its platform rank badged at the corner.
///
/// The top three take the cabal's own `cta` pair — the deeper tint, because a 13pt bold numeral
/// is not "large text" under WCAG and `fill` only clears the 3:1 large-text bar. Rank 1 carries
/// a trophy instead of a "1". Everything below third is a grey numeral: a board where every row
/// is decorated has no podium.
private struct RankedCabalMark: View {
    let rank: Int
    let tint: MonacoTheme.CabalTint
    let name: String

    private var isPodium: Bool { rank <= 3 }

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            CabalMark(tint: tint, name: name, size: 40)
                .accessibilityHidden(true)
            badge
                .offset(x: 5, y: 5)
                .accessibilityLabel("Rank \(rank)")
        }
    }

    @ViewBuilder
    private var badge: some View {
        if rank == 1 {
            Image(systemName: "trophy.fill")
                .font(.system(size: 9, weight: .bold))
                .foregroundStyle(Color.white)
                .frame(width: 18, height: 18)
                .background(Circle().fill(tint.cta))
                .overlay(Circle().strokeBorder(MonacoTheme.bgRaised, lineWidth: 1.5))
        } else {
            Text("\(rank)")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(isPodium ? Color.white : MonacoTheme.fgMuted)
                .frame(minWidth: 18, minHeight: 18)
                .background(Circle().fill(isPodium ? tint.cta : MonacoTheme.fillQuiet))
                .overlay(Circle().strokeBorder(MonacoTheme.bgRaised, lineWidth: 1.5))
        }
    }
}
