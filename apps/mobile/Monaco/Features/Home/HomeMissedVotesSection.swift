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

    /// Whether the vote closes inside the hour, which is what earns the amber ring.
    static func isClosingSoon(expiresAt: Date, now: Date = Date()) -> Bool {
        let remaining = expiresAt.timeIntervalSince(now)
        return remaining > 0 && remaining <= 3600
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

/// **"Needs your vote" — Home's one ink band**, and the product's highest-intent social moment.
///
/// It was a grey `MonacoRow` reading "Weekend investors · closes in 4h" with a chevron. It is now
/// a `viewAligned`-snapping deck of ink cards carrying the stock, the cabal in its own tint, the
/// live countdown and **Yes / No inline**: the whole point of Monaco, one tap from the top of Home.
///
/// **What is deliberately absent.** `HomeMissedProposalRowDTO` carries `groupID`, `groupName`,
/// `proposalID`, `symbol`, `status`, `createdAt` and `expiresAt` — and nothing else. There is no
/// amount, no tally and no voter list, so this deck shows none of the three. A fabricated face or
/// a made-up "2 of 5" on a screen that spends real money is worse than a card that says less.
/// `amountMicros`, `voteSummary` and `yesVoters` are filed as the API follow-up.
///
/// At `dynamicTypeSize.isAccessibilitySize` the deck falls back to a vertical stack, exactly as
/// `StockMoverStrip.isAvailable` already does for the movers strip.
struct HomeMissedVotesSection: View {
    /// Already filtered to the rows still open; `HomeView` owns that clock.
    let rows: [HomeMissedProposalRowDTO]
    let onOpen: (String) -> Void
    /// Casts a ballot straight from the deck. Home owns the service and the refresh.
    var onVote: (HomeMissedProposalRowDTO, ProposalVoteChoice) async -> Bool = { _, _ in false }

    /// Fixed on first appearance. `.now` inside the schedule would re-anchor its phase on
    /// every body pass — every dashboard write — so the tick would not be a stable minute.
    @State private var tickAnchor = Date()
    /// Proposals this member has voted on from the deck. The card swaps to a confirmation rather
    /// than vanishing under the finger that just voted; Home's next poll drops the row for real.
    @State private var voted: [String: ProposalVoteChoice] = [:]

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var usesDeck: Bool { !dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        // The dashboard is polled at rest, so without a clock of its own the countdown sits
        // frozen at whatever it said when the rows arrived.
        TimelineView(.periodic(from: tickAnchor, by: 60)) { context in
            section(now: context.date)
        }
    }

    private func section(now: Date) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            Text("Needs your vote")
                .displayFont(.eyebrow)
                .foregroundStyle(MonacoTheme.Ink.fgSubtle)

            if usesDeck {
                deck(now: now)
            } else {
                VStack(spacing: MonacoTheme.Space.sm) {
                    ForEach(rows, id: \.proposalId) { row in
                        card(for: row, now: now)
                    }
                }
            }
        }
        .monacoInkBand()
    }

    private func deck(now: Date) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: MonacoTheme.Space.sm) {
                ForEach(rows, id: \.proposalId) { row in
                    card(for: row, now: now)
                        .frame(width: 268)
                }
            }
            .scrollTargetLayout()
            // The band is full-bleed, so the deck scrolls edge to edge and the first card still
            // starts on the gutter.
            .padding(.horizontal, MonacoTheme.Space.gutter)
        }
        .scrollTargetBehavior(.viewAligned)
        .padding(.horizontal, -MonacoTheme.Space.gutter)
    }

    private func card(for row: HomeMissedProposalRowDTO, now: Date) -> some View {
        HomeMissedVoteCard(
            row: row,
            now: now,
            castChoice: voted[row.proposalId],
            onOpen: { onOpen(row.proposalId) },
            onVote: { choice in
                let ok = await onVote(row, choice)
                guard ok else { return }
                withAnimation(MonacoMotion.glide.reduced(reduceMotion)) {
                    voted[row.proposalId] = choice
                }
            }
        )
    }
}

/// One ink card in the deck: the stock, the cabal in its own tint, the countdown, Yes / No.
private struct HomeMissedVoteCard: View {
    let row: HomeMissedProposalRowDTO
    let now: Date
    let castChoice: ProposalVoteChoice?
    let onOpen: () -> Void
    let onVote: (ProposalVoteChoice) async -> Void

    @State private var isVoting = false

    private var tint: MonacoTheme.CabalTint { .forGroupId(row.groupId) }

    private var stockName: String {
        AssetDisplayNames.name(forSymbol: row.symbol) ?? AssetSymbolFormatter.display(row.symbol)
    }

    private var countdown: String? {
        HomeVoteCountdown.label(expiresAt: row.expiresAt, now: now)
    }

    private var isClosingSoon: Bool {
        HomeVoteCountdown.isClosingSoon(expiresAt: row.expiresAt, now: now)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            Button(action: onOpen) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    HStack(spacing: MonacoTheme.Space.sm) {
                        StockMark(symbol: row.symbol, size: 40)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(stockName)
                                .displayFont(.section)
                                .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                                .lineLimit(1)
                                .minimumScaleFactor(0.8)
                            Text(AssetSymbolFormatter.display(row.symbol))
                                .font(MonacoTheme.Typo.caption)
                                .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                        }
                        Spacer(minLength: MonacoTheme.Space.s)
                        // Closing soon is a header-trailing chip, not a grey footnote.
                        if let countdown {
                            countdownChip(countdown)
                        }
                    }

                    HStack(spacing: MonacoTheme.Space.s) {
                        CabalMark(groupId: row.groupId, name: row.groupName, size: 18, onInk: true)
                        Text(row.groupName)
                            .font(MonacoTheme.Typo.caption.weight(.semibold))
                            .foregroundStyle(tint.onInk)
                            .lineLimit(1)
                        Spacer(minLength: 0)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(stockName), \(row.groupName)\(countdown.map { ", \($0)" } ?? "")")
            .accessibilityHint("Opens the proposal")
            .accessibilityIdentifier("home-missed-\(row.proposalId)")

            ballot
        }
        .padding(MonacoTheme.Space.m)
        .monacoElevation(.card)
        .overlay {
            // The amber ring is the last hour, and nothing else on this card carries amber.
            if isClosingSoon {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous)
                    .strokeBorder(MonacoTheme.warningOnInk.opacity(0.65), lineWidth: 1)
            }
        }
    }

    private func countdownChip(_ text: String) -> some View {
        Text(text)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(isClosingSoon ? MonacoTheme.warningOnInk : MonacoTheme.Ink.fgMuted)
            .lineLimit(1)
            .padding(.horizontal, MonacoTheme.Space.s)
            .padding(.vertical, 4)
            .background {
                if isClosingSoon {
                    Capsule().fill(MonacoTheme.warningWashOnInk)
                }
            }
    }

    @ViewBuilder
    private var ballot: some View {
        if let castChoice {
            Text(castChoice == .yes ? "You voted yes" : "You voted no")
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(MonacoTheme.Ink.fgMuted)
                .frame(maxWidth: .infinity, minHeight: 44)
                .transition(.opacity)
                .accessibilityIdentifier("home-missed-voted-\(row.proposalId)")
        } else {
            HStack(spacing: MonacoTheme.Space.s) {
                voteButton(.yes, title: "Yes")
                voteButton(.no, title: "No")
            }
            .disabled(isVoting)
            .opacity(isVoting ? 0.6 : 1)
        }
    }

    private func voteButton(_ choice: ProposalVoteChoice, title: String) -> some View {
        Button {
            Task {
                guard !isVoting else { return }
                isVoting = true
                await onVote(choice)
                isVoting = false
            }
        } label: {
            Text(title)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(choice == .yes ? MonacoTheme.onBrand : MonacoTheme.Ink.fgPrimary)
                .frame(maxWidth: .infinity, minHeight: 44)
                .background {
                    if choice == .yes {
                        Capsule().fill(MonacoTheme.brandFill)
                    } else {
                        Capsule().strokeBorder(MonacoTheme.Ink.lineStrong, lineWidth: 1)
                    }
                }
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Vote \(title.lowercased()) on \(stockName)")
        .accessibilityIdentifier("home-missed-vote-\(choice.rawValue)-\(row.proposalId)")
    }
}
