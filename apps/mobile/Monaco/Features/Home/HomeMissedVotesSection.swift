import MonacoCore
import SwiftUI

/// How long a vote has left, in the row's subtitle.
///
/// The old copy floored the hours, so 1 h 59 m read "closes in 1h" and a three-day vote read
/// "closes in 71h"; a vote that had already closed read "closing" until the next poll (#327).
enum HomeVoteCountdown {
    /// Nil once the vote has closed — the row is dropped rather than relabelled.
    ///
    /// Each unit is promoted rather than allowed to overflow: rounding up inside minutes would
    /// otherwise print "closes in 60m" at 59 m 59 s, and rounding inside hours "closes in 24h"
    /// at 23 h 59 m 59 s. Neither is a unit the member ever sees anywhere else.
    static func label(expiresAt: Date, now: Date = Date()) -> String? {
        let remaining = expiresAt.timeIntervalSince(now)
        guard remaining > 0 else { return nil }
        if remaining < 60 { return "closes in under a minute" }

        let minutes = Int(ceil(remaining / 60))
        if minutes < 60 { return "closes in \(minutes)m" }

        let hours = Int((remaining / 3600).rounded())
        if hours < 24 { return "closes in \(max(1, hours))h" }

        return "closes in \(max(1, Int(ceil(remaining / 86_400))))d"
    }
}

/// Which of Home's "Needs your vote" rows are still collecting votes.
///
/// Home polls at the resting cadence, so rows go on expiring between payloads. Keeping this
/// separate from the view lets Home gate the section on the open rows — a section that renders
/// nothing still takes a `VStack` spacing on each side, which is a doubled gap under the
/// balance row until the next dashboard write.
enum HomeMissedVotes {
    static func open(_ rows: [HomeMissedProposalRowDTO], now: Date) -> [HomeMissedProposalRowDTO] {
        rows.filter { $0.expiresAt > now }
    }

    /// When the open set next changes — the earliest expiry still ahead, or nil when every row
    /// has already closed. Waking exactly then keeps Home's body out of a 60-second loop.
    static func nextExpiry(_ rows: [HomeMissedProposalRowDTO], after now: Date) -> Date? {
        rows.map(\.expiresAt).filter { $0 > now }.min()
    }
}

/// "Needs your vote": plain rows, not compact proposal cards. `HomeMissedProposalRowDTO`
/// carries no amount or tally, so this only ever routes to the real detail screen.
/// The section is not rendered at all when nothing is open — no "caught up" card.
/// Rows open the proposal through `HomeView`'s `navigationDestination`, not through a
/// `NavigationLink` of their own. Two reasons: a destination closure inside this section's
/// body would be re-evaluated by the countdown's tick — rebuilding a pushed
/// `ProposalDetailView` with a fresh `LiveProposalFeedService` under a member who is reading
/// or voting — and a destination declared here would be torn down, popping that screen, the
/// moment the last row expires and Home stops rendering the section.
struct HomeMissedVotesSection: View {
    /// Already filtered to the rows still open; `HomeView` owns that clock.
    let rows: [HomeMissedProposalRowDTO]
    let onOpen: (String) -> Void

    /// Fixed on first appearance. `.now` inside the schedule would re-anchor its phase on
    /// every body pass — every dashboard write — so the tick would not be a stable minute.
    @State private var tickAnchor = Date()

    var body: some View {
        // The dashboard is polled at rest, so without a clock of its own the countdown sits
        // frozen at whatever it said when the rows arrived.
        TimelineView(.periodic(from: tickAnchor, by: 60)) { context in
            section(now: context.date)
        }
    }

    private func section(now: Date) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Needs your vote")

            MonacoGroupedList {
                ForEach(rows, id: \.proposalId) { (row: HomeMissedProposalRowDTO) in
                    Button {
                        onOpen(row.proposalId)
                    } label: {
                        MonacoRow(
                            title: AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol),
                            subtitle: subtitle(for: row, now: now),
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

    private func subtitle(for row: HomeMissedProposalRowDTO, now: Date) -> String {
        guard let closes = HomeVoteCountdown.label(expiresAt: row.expiresAt, now: now) else {
            return row.groupName
        }
        return "\(row.groupName) · \(closes)"
    }
}
