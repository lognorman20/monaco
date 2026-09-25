import MonacoCore
import SwiftUI

/// One joined cabal on Profile: the cabal's pot from `/v1/home` plus the viewer's
/// position from `/v1/home/dashboard`. Both are already in `AppSessionStore`.
struct ProfileCabalRow: Identifiable, Equatable {
    let groupId: String
    let name: String
    let potValueUsd: String
    /// The cabal's picture; nil draws its tinted initials.
    var pictureUrl: String?
    let equityUsd: String?
    let dollarPnl: String?
    let percentReturn: String?

    var id: String { groupId }

    static func rows(home: HomeViewDTO?, dashboard: HomeDashboardDTO?) -> [ProfileCabalRow] {
        let positions = Dictionary(
            (dashboard?.myGroups ?? []).map { ($0.groupId, $0) },
            uniquingKeysWith: { first, _ in first }
        )
        return (home?.groups ?? [])
            .filter(\.isJoined)
            .map { group in
                let position = positions[group.groupId]
                return ProfileCabalRow(
                    groupId: group.groupId,
                    name: group.name,
                    potValueUsd: group.potValueUsd,
                    pictureUrl: group.pictureUrl,
                    equityUsd: position?.equityUsd,
                    dollarPnl: position?.dollarPnl,
                    percentReturn: position?.percentReturn
                )
            }
    }

    /// Nil when the dashboard has no position for this cabal yet.
    var figures: CabalPositionRowFigures? {
        guard let equityUsd, let dollarPnl else { return nil }
        return CabalPositionRowFigures(equityUsd: equityUsd, dollarPnl: dollarPnl, percentReturn: percentReturn)
    }
}

struct ProfileCabalsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [ProfileCabalRow]
    var onLeft: () async -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Your cabals")
                .padding(.horizontal, MonacoTheme.Space.m)

            if rows.isEmpty {

                EmptyState(
                    title: "No cabals yet",
                    message: "Start a cabal or join one from the Cabals tab."
                )
                .accessibilityIdentifier("profile-cabals-empty")
            } else {
                MonacoGroupedList {
                    ForEach(rows) { row in
                        NavigationLink {
                            GroupDetailView(
                                auth: auth,
                                groupId: row.groupId,
                                groupName: row.name,
                                onLeft: onLeft
                            )
                        } label: {
                            CabalPositionRow(
                                groupId: row.groupId,
                                name: row.name,
                                pictureUrl: row.pictureUrl,
                                potValueUsd: row.potValueUsd,
                                figures: row.figures,
                                isLast: row.groupId == rows.last?.groupId
                            )
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("profile-cabal-\(row.groupId)")
                    }
                }
            }
        }
    }
}
