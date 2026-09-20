import MonacoCore
import SwiftUI

/// One proposal: what the cabal would buy or sell, who wants it and why, where the vote stands,
/// and Yes / No while the viewer may still vote. Read-only proposals (seeded, or the viewer is not
/// in the voter set) show the tally only.
///
/// Used by `ProposalFeedView` and Group detail (summary links to detail) and as the header of
/// `ProposalDetailView` (no link, no card chrome).
struct ProposalCardView<Destination: View>: View {
    let proposal: ProposalDTO
    var isVoting = false
    var onVote: (ProposalVoteChoice) -> Void = { _ in }
    /// Detail screen pushed when the summary is tapped; nil when the card is the detail header.
    var destination: (() -> Destination)?
    /// Prefix of the thesis excerpt's accessibility identifier; Group detail keeps its own.
    var thesisIdentifierPrefix = "proposal-card-reason"
    /// The viewer's ballot when known ("yes" / "no"): replaces the buttons with "You voted yes".
    var viewerChoice: String?
    /// False for the detail header, which sits directly on the canvas.
    var showsChrome = true
    /// Pulses an ink stroke once, for a card that just arrived.
    var highlight = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var pulse = false

    init(
        proposal: ProposalDTO,
        isVoting: Bool = false,
        onVote: @escaping (ProposalVoteChoice) -> Void = { _ in },
        destination: (() -> Destination)?,
        thesisIdentifierPrefix: String = "proposal-card-reason",
        viewerChoice: String? = nil,
        showsChrome: Bool = true,
        highlight: Bool = false
    ) {
        self.proposal = proposal
        self.isVoting = isVoting
        self.onVote = onVote
        self.destination = destination
        self.thesisIdentifierPrefix = thesisIdentifierPrefix
        self.viewerChoice = viewerChoice
        self.showsChrome = showsChrome
        self.highlight = highlight
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            if let destination {
                NavigationLink {
                    destination()
                } label: {
                    summary
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("proposal-card-open-\(proposal.id)")
            } else {
                summary
            }

            actions
        }
        .padding(showsChrome ? MonacoTheme.Space.m : 0)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            if showsChrome {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .fill(MonacoTheme.surface)
            }
        }
        .overlay {
            if showsChrome {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .strokeBorder(MonacoTheme.ink, lineWidth: 1)
                    .opacity(pulse ? 1 : 0)
            }
        }
        .task(id: highlight) {
            guard highlight, !reduceMotion else { return }
            withAnimation(.easeOut(duration: 0.2)) { pulse = true }
            try? await Task.sleep(for: .seconds(1))
            withAnimation(.easeIn(duration: 0.4)) { pulse = false }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-card-\(proposal.id)")
    }

    // MARK: Summary

    private var summary: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            header
            amount
            reason
            if let summary = proposal.voteSummary {
                ProposalVoteTally(progress: ProposalVoteProgress(summary: summary), isOpen: proposal.isOpen)
                    .accessibilityIdentifier("proposal-card-votes-\(proposal.id)")
            }
            footer
        }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
            mark
            VStack(alignment: .leading, spacing: 2) {
                Text(ProposalFeedCopy.title(for: proposal))
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text(ProposalFeedCopy.subtitle(for: proposal))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: MonacoTheme.Space.s)
            if let closed = ProposalFeedCopy.closedLabel(for: proposal) {
                ProposalStatusChip(label: closed, stage: ProposalExecutionStage.of(proposal), status: proposal.status, kind: proposal.resolvedKind)
            } else if let createdAt = proposal.createdAt {
                Text(RelativeTimeFormatter.label(iso: createdAt))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.tertiaryText)
            }
        }
    }

    @ViewBuilder
    private var mark: some View {
        if proposal.isTrade {
            StockMark(symbol: proposal.symbol, size: 40)
        } else {
            StockMark(systemImage: "cpu", size: 40)
        }
    }

    @ViewBuilder
    private var amount: some View {
        Group {
            switch proposal.resolvedKind {
            case "sell":
                Text(ProposalShareFormatter.sharesLabel(fromAtomics: proposal.tokenAmount ?? "0"))
                    .font(MonacoTheme.Typo.moneyLarge)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
            case "buy":
                if let micros = proposal.usdcMicros.flatMap({ Int64($0) }) {
                    MoneyText(micros: micros, style: .large)
                }
            case "add_agent":
                if let micros = proposal.allocationUsdcMicros.flatMap({ Int64($0) }) {
                    MoneyText(micros: micros, style: .large)
                }
            default:
                EmptyView()
            }
        }
        .accessibilityIdentifier("proposal-card-amount-\(proposal.id)")
    }

    /// The proposer's reason with their name in front, or just who proposed it.
    @ViewBuilder
    private var reason: some View {
        let name = proposal.proposerName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let thesis = proposal.thesis?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        // The detail screen quotes the full reason below the header, so the header only names the proposer.
        if !thesis.isEmpty, showsChrome {
            // Two-line excerpt; the full reason lives on the detail screen.
            (nameLead(name) + Text(thesis).foregroundStyle(MonacoTheme.muted))
                .font(MonacoTheme.Typo.callout)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("\(thesisIdentifierPrefix)-\(proposal.id)")
        } else if !name.isEmpty {
            Text(ProposalFeedCopy.proposedBy(name))
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    private func nameLead(_ name: String) -> Text {
        name.isEmpty ? Text("") : Text(name + "  ").fontWeight(.semibold).foregroundStyle(MonacoTheme.ink)
    }

    private var footer: some View {
        HStack(spacing: MonacoTheme.Space.m) {
            if proposal.isOpen, let expiresAt = proposal.expiresAt,
               let closes = ProposalTimeFormatter.closesLabel(expiresAt: expiresAt) {
                let soon = ProposalTimeFormatter.closesSoon(expiresAt: expiresAt)
                Label(closes, systemImage: "clock")
                    .foregroundStyle(soon ? MonacoTheme.warning : MonacoTheme.muted)
            }
            Spacer(minLength: 0)
            if let count = proposal.commentCount, count > 0 {
                Label("\(count)", systemImage: "bubble.left")
                    .foregroundStyle(MonacoTheme.muted)
                    .accessibilityLabel(ProposalFeedCopy.commentCount(count))
                    .accessibilityIdentifier("proposal-card-comment-count-\(proposal.id)")
            }
        }
        .font(MonacoTheme.Typo.caption.monospacedDigit())
        .labelStyle(ProposalFooterLabelStyle())
    }

    // MARK: Actions

    @ViewBuilder
    private var actions: some View {
        if let viewerChoice {
            HStack(spacing: MonacoTheme.Space.s) {
                Image(systemName: "checkmark")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
                    .frame(width: 20, height: 20)
                    .background(Circle().fill(viewerChoice.lowercased() == "no" ? MonacoTheme.loss : MonacoTheme.ink))
                Text(ProposalFeedCopy.viewerVoted(viewerChoice))
                    .font(MonacoTheme.Typo.callout.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
            }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                .transition(.opacity)
                .accessibilityIdentifier("proposal-card-voted-\(proposal.id)")
        } else if proposal.showsVoteActions {
            HStack(spacing: MonacoTheme.Space.s) {
                Button {
                    Haptics.tap()
                    onVote(.yes)
                } label: {
                    Text(ProposalFeedCopy.voteYes)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("proposal-card-vote-yes-\(proposal.id)")

                Button {
                    Haptics.tap()
                    onVote(.no)
                } label: {
                    Text(ProposalFeedCopy.voteNo)
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("proposal-card-vote-no-\(proposal.id)")
            }
            .monacoFullWidthButtons()
            .disabled(isVoting)
            .opacity(isVoting ? 0.6 : 1)
            .transition(.opacity)
        }
    }
}

extension ProposalCardView where Destination == EmptyView {
    /// Card without a detail link.
    init(
        proposal: ProposalDTO,
        isVoting: Bool = false,
        onVote: @escaping (ProposalVoteChoice) -> Void = { _ in },
        viewerChoice: String? = nil,
        showsChrome: Bool = true
    ) {
        self.init(
            proposal: proposal,
            isVoting: isVoting,
            onVote: onVote,
            destination: nil,
            viewerChoice: viewerChoice,
            showsChrome: showsChrome
        )
    }
}

/// Dots (up to 12 voters) and "2 of 5 voted · 3 yes to pass". Brand = yes, loss outline = no,
/// hairline = still to vote. Green stays reserved for profit.
struct ProposalVoteTally: View {
    let progress: ProposalVoteProgress
    var isOpen = true

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var caption: String {
        isOpen ? progress.caption : progress.closedCaption
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            if let dots = progress.dots {
                HStack(spacing: 4) {
                    ForEach(Array(dots.enumerated()), id: \.offset) { _, dot in
                        VoteDot(dot: dot)
                    }
                }
                .animation(reduceMotion ? nil : .spring(response: 0.35, dampingFraction: 0.6), value: dots)
                .accessibilityHidden(true)
            }
            Text(caption)
                .font(MonacoTheme.Typo.caption.monospacedDigit())
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
                .contentTransition(reduceMotion ? .identity : .numericText())
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(caption)
    }
}

private struct VoteDot: View {
    let dot: ProposalVoteDot

    var body: some View {
        Circle()
            .fill(dot == .yes ? MonacoTheme.brand : Color.clear)
            .overlay {
                Circle().strokeBorder(stroke, lineWidth: dot == .no ? 1.5 : 1)
            }
            .frame(width: 8, height: 8)
            .scaleEffect(dot == .pending ? 1 : 1.12)
    }

    private var stroke: Color {
        switch dot {
        case .yes: MonacoTheme.brand
        case .no: MonacoTheme.loss
        case .pending: MonacoTheme.tertiaryText.opacity(0.7)
        }
    }
}

/// Icon and text tight together, icon a touch smaller.
private struct ProposalFooterLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.icon.imageScale(.small)
            configuration.title
        }
    }
}
