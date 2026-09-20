import MonacoCore
import SwiftUI

/// "Needs your vote": plain rows, not compact proposal cards. `HomeMissedProposalRowDTO`
/// carries no amount or tally, so this only ever routes to the real detail screen.
/// The section is not rendered at all when `rows` is empty — no "caught up" card.
struct HomeMissedVotesSection: View {
    @ObservedObject var auth: DynamicAuthService
    let rows: [HomeMissedProposalRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Needs your vote")

            MonacoGroupedList {
                ForEach(rows, id: \.proposalId) { (row: HomeMissedProposalRowDTO) in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        MonacoRow(
                            title: AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol),
                            subtitle: "\(row.groupName) · \(closesInLabel(row.expiresAt))",
                            chevron: true,
                            isLast: row.proposalId == rows.last?.proposalId
                        ) {
                            StockMark(symbol: row.symbol)
                        }
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("home-missed-\(row.proposalId)")
                }
            }
        }
    }

    /// Interim countdown copy — the frozen `RelativeTimeFormatter` handles past timestamps,
    /// not this future "closes in" countdown, so it stays local to Home.
    private func closesInLabel(_ expiresAt: Date, now: Date = Date()) -> String {
        let remaining = expiresAt.timeIntervalSince(now)
        guard remaining > 0 else { return "closing" }
        let hours = Int(remaining / 3600)
        if hours >= 1 { return "closes in \(hours)h" }
        let minutes = max(1, Int(remaining / 60))
        return "closes in \(minutes)m"
    }
}
