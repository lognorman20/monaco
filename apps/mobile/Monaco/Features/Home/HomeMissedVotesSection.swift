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

/// "Needs your vote": a counted header and ruled rows, each ending in an ink "Vote".
/// `HomeMissedProposalRowDTO` carries no amount or tally, so this only ever routes to the real
/// detail screen, where the ballot is cast.
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
            MonacoSectionHeader("Needs your vote", count: rows.count)
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                ForEach(rows, id: \.proposalId) { (row: HomeMissedProposalRowDTO) in
                    Button {
                        onOpen(row.proposalId)
                    } label: {
                        HomeMissedVoteRow(row: row, now: now, isLast: row.proposalId == rows.last?.proposalId)
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("home-missed-\(row.proposalId)")
                }
            }
        }
    }
}

/// One open vote: the stock's coin and ticker, the cabal and the countdown, and the word
/// "Vote" in ink on the right — the row is the errand and says so.
private struct HomeMissedVoteRow: View {
    let row: HomeMissedProposalRowDTO
    let now: Date
    let isLast: Bool

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var closes: String? {
        HomeVoteCountdown.label(expiresAt: row.expiresAt, now: now)
    }

    /// At the accessibility sizes the cabal and the countdown get two lines and the word
    /// "Vote" drops under them, the way every `MonacoRow` stacks.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        mark
                        labels(lineLimit: 2)
                    }
                    voteBadge
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    mark
                    labels(lineLimit: 1)
                    voteBadge
                }
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)

        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(spoken)
    }

    private var mark: some View {
        StockMark(symbol: row.symbol)
            .frame(width: 44, height: 44)
    }

    private func labels(lineLimit: Int) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(AssetSymbolFormatter.display(row.symbol))
                .font(MonacoTheme.Typo.ticker)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
            subtitle
                .lineLimit(lineLimit)
                .truncationMode(.middle)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var voteBadge: some View {
        Text("Vote")
            .font(MonacoTheme.Typo.captionStrong)
            .foregroundStyle(MonacoTheme.onBrand)
            .padding(.horizontal, 12)
            .frame(minHeight: 30)
            .background(Capsule().fill(MonacoTheme.brandFill))
            .accessibilityHidden(true)
    }

    /// The cabal in the brand voice, the countdown in the market's, so the deadline reads as
    /// the clock it is.

    private var subtitle: Text {
        let cabal = Text(row.groupName)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.muted)
        guard let closes else { return cabal }
        return cabal
            + Text("  ·  ").font(MonacoTheme.Typo.caption).foregroundStyle(MonacoTheme.tertiaryText)
            + Text(closes).font(MonacoTheme.Typo.stamp).foregroundStyle(MonacoTheme.tertiaryText)
    }

    private var spoken: String {
        var sentence = "\(AssetSymbolFormatter.display(row.symbol)), \(row.groupName)"
        if let closes { sentence += ", \(closes)" }
        return sentence + ". Vote."
    }
}
