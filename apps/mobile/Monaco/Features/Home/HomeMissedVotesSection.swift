import MonacoCore
import SwiftUI

struct HomeMissedVotesSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMissedProposalRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Needs your vote")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if rows.isEmpty {
                MonacoEmptyStateCard(
                    message: "You're caught up.",
                    systemImage: "checkmark.circle"
                )
            } else {
                ForEach(rows) { row in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        MonacoRowCard(
                            systemImage: "hand.raised.fill",
                            title: CatalogAssetNameFormatter.format(AssetSymbolFormatter.format(row.symbol)),
                            subtitle: row.groupName,
                            trailing: "Vote"
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-missed-\(row.proposalId)")
                }
            }
        }
    }
}
