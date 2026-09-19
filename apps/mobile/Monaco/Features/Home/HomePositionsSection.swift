import MonacoCore
import SwiftUI

struct HomePositionsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMyGroupRowDTO]
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
                            MonacoRow(
                                title: row.name,
                                subtitle: "Your slice \(UsdAmountFormatter.format(decimalString: row.equityUsd)) · \(SlicePercentFormatter.format(row.slicePercent))",
                                chevron: true,
                                isLast: row.groupId == rows.last?.groupId,
                                leading: { CabalMark(groupId: row.groupId, name: row.name) },
                                trailing: {
                                    PnLText(dollarPnl: row.dollarPnl, style: .row)
                                    PercentText(percentReturn: row.percentReturn, style: .caption)
                                }
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
