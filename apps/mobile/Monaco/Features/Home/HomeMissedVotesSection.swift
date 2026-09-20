import MonacoCore
import SwiftUI

/// How long a vote has left, in the row's subtitle.
///
/// The old copy floored the hours, so 1 h 59 m read "closes in 1h" and a three-day vote read
/// "closes in 71h"; a vote that had already closed read "closing" until the next poll (#327).
enum HomeVoteCountdown {
    /// Nil once the vote has closed — the row is dropped rather than relabelled.
    static func label(expiresAt: Date, now: Date = Date()) -> String? {
        let remaining = expiresAt.timeIntervalSince(now)
        guard remaining > 0 else { return nil }
        if remaining < 60 { return "closes in under a minute" }
        if remaining < 3600 { return "closes in \(Int(ceil(remaining / 60)))m" }
        if remaining < 86_400 { return "closes in \(max(1, Int((remaining / 3600).rounded())))h" }
        return "closes in \(Int(ceil(remaining / 86_400)))d"
    }
}

/// "Needs your vote": plain rows, not compact proposal cards. `HomeMissedProposalRowDTO`
/// carries no amount or tally, so this only ever routes to the real detail screen.
/// The section is not rendered at all when nothing is open — no "caught up" card.
struct HomeMissedVotesSection: View {
    @ObservedObject var auth: PrivyAuthService
    let rows: [HomeMissedProposalRowDTO]

    var body: some View {
        // The dashboard is polled at rest, so without a clock of its own the countdown sits
        // frozen at whatever it said when the rows arrived.
        TimelineView(.periodic(from: .now, by: 60)) { context in
            let open = rows.filter { $0.expiresAt > context.date }
            if !open.isEmpty {
                section(open, now: context.date)
            }
        }
    }

    private func section(_ open: [HomeMissedProposalRowDTO], now: Date) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Needs your vote")

            MonacoGroupedList {
                ForEach(open, id: \.proposalId) { (row: HomeMissedProposalRowDTO) in
                    NavigationLink {
                        ProposalDetailView(auth: auth, proposalId: row.proposalId)
                    } label: {
                        MonacoRow(
                            title: AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol),
                            subtitle: subtitle(for: row, now: now),
                            chevron: true,
                            isLast: row.proposalId == open.last?.proposalId
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

    private func subtitle(for row: HomeMissedProposalRowDTO, now: Date) -> String {
        guard let closes = HomeVoteCountdown.label(expiresAt: row.expiresAt, now: now) else {
            return row.groupName
        }
        return "\(row.groupName) · \(closes)"
    }
}
