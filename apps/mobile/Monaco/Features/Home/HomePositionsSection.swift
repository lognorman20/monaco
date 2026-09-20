import MonacoCore
import SwiftUI

struct HomePositionsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMyGroupRowDTO]
    /// Pot value per cabal from `/v1/home`, which lands after the dashboard; rows show their
    /// "Pot …" subtitle once it has.
    var potValuesUsd: [String: String] = [:]
    var onLeft: () async -> Void = {}
    var onBrowseCabals: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Your cabals")

            if rows.isEmpty {
                EmptyState(
                    title: "No cabals yet",
                    message: "Start one with friends or join an open one.",
                    actionTitle: "Browse cabals",
                    action: onBrowseCabals
                )
                .accessibilityIdentifier("home-cabals-empty")
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
                                potValueUsd: potValuesUsd[row.groupId],
                                figures: CabalPositionRowFigures(
                                    equityUsd: row.equityUsd,
                                    dollarPnl: row.dollarPnl,
                                    percentReturn: row.percentReturn
                                ),
                                isLast: row.groupId == rows.last?.groupId
                            )
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("home-my-group-\(row.groupId)")
                    }
                }
            }
        }
    }
}

/// One "Your cabals" row, shared by Home and Profile so the two lists cannot drift apart:
/// the cabal and its pot on the left, the member's own money on the right with the change
/// since they joined underneath.
struct CabalPositionRow: View {
    let groupId: String
    let name: String
    /// The cabal's picture; nil draws its tinted initials.
    var pictureUrl: String? = nil
    let potValueUsd: String?
    /// Nil while the member's position has not loaded; the row then shows no figures.
    let figures: CabalPositionRowFigures?
    let isLast: Bool

    var body: some View {
        MonacoRow(
            title: name,
            subtitle: CabalPositionRowFigures.potSubtitle(potValueUsd: potValueUsd),
            chevron: true,
            isLast: isLast,
            leading: { CabalMark(groupId: groupId, name: name, pictureUrl: pictureUrl) },
            trailing: {
                if let figures {
                    MoneyText(decimalString: figures.equityUsd, style: .row)
                        .accessibilityLabel("Your slice \(UsdAmountFormatter.format(decimalString: figures.equityUsd))")
                    switch figures.change {
                    case .percent(let percentReturn):
                        PercentText(percentReturn: percentReturn, style: .caption)
                    case .dollars(let dollarPnl):
                        PnLText(dollarPnl: dollarPnl, style: .caption)
                    case .unavailable:
                        PercentText(percentReturn: nil, style: .caption)
                    }
                }
            }
        )
    }
}
