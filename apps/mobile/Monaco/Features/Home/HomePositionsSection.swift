import MonacoCore
import SwiftUI

struct HomePositionsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMyGroupRowDTO]
    var onLeft: () async -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your positions")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if rows.isEmpty {
                MonacoEmptyStateCard(
                    message: "Join a cabal to see your positions here.",
                    systemImage: "person.3"
                )
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
                            subtitle: "\(UsdAmountFormatter.format(decimalString: row.equityUsd)) · \(SlicePercentFormatter.format(row.slicePercent)) of pot",
                            trailing: "\(PercentReturnFormatter.format(row.percentReturn))  \(row.dollarPnl)"
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-my-group-\(row.groupId)")
                }
            }
        }
    }
}
