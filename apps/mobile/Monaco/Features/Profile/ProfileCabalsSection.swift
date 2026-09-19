import MonacoCore
import SwiftUI

/// One joined cabal on Profile: the cabal's pot from `/v1/home` plus the viewer's
/// position from `/v1/home/dashboard`. Both are already in `AppSessionStore`.
struct ProfileCabalRow: Identifiable, Equatable {
    let groupId: String
    let name: String
    let potValueUsd: String
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
                    equityUsd: position?.equityUsd,
                    dollarPnl: position?.dollarPnl,
                    percentReturn: position?.percentReturn
                )
            }
    }

    var subtitle: String {
        "Pot \(UsdAmountFormatter.format(decimalString: potValueUsd))"
    }
}

struct ProfileCabalsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [ProfileCabalRow]
    var onLeft: () async -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Your cabals")

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
                            MonacoRow(
                                title: row.name,
                                subtitle: row.subtitle,
                                chevron: true,
                                isLast: row.groupId == rows.last?.groupId,
                                leading: { CabalMark(groupId: row.groupId, name: row.name) },
                                trailing: {
                                    if let dollarPnl = row.dollarPnl {
                                        PnLText(dollarPnl: dollarPnl, style: .row)
                                        PercentText(percentReturn: row.percentReturn, style: .caption)
                                    }
                                }
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
