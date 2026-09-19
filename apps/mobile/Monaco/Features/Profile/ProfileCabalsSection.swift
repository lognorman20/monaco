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
        let pot = "Pot \(UsdAmountFormatter.format(decimalString: potValueUsd))"
        guard let equityUsd else { return pot }
        return "\(pot) · You \(UsdAmountFormatter.format(decimalString: equityUsd))"
    }

    var trailing: String? {
        guard let dollarPnl else { return nil }
        return "\(PercentReturnFormatter.format(percentReturn))  \(dollarPnl)"
    }
}

struct ProfileCabalsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [ProfileCabalRow]
    var onLeft: () async -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your cabals")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if rows.isEmpty {
                MonacoEmptyStateCard(
                    message: "Join or create a cabal from Home to see it here.",
                    systemImage: "person.3"
                )
                .accessibilityIdentifier("profile-cabals-empty")
            } else {
                ForEach(rows) { row in
                    NavigationLink {
                        GroupDetailView(
                            auth: auth,
                            groupId: row.groupId,
                            groupName: row.name,
                            onLeft: onLeft
                        )
                    } label: {
                        MonacoRowCard(
                            systemImage: "person.3.fill",
                            title: row.name,
                            subtitle: row.subtitle,
                            trailing: row.trailing
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("profile-cabal-\(row.groupId)")
                }
            }
        }
    }
}
