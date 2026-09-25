import MonacoCore
import SwiftUI

/// One proposal: what the cabal would buy or sell, who wants it and why, where the vote stands,
/// and Yes / No while the viewer may still vote. Read-only proposals (seeded, or the viewer is not
/// in the voter set) show the tally only.
///
/// Used by `ProposalFeedView` and Group detail (summary links to detail) and as the header of
/// `ProposalDetailView` (no link, no card chrome).
///
/// It reads top to bottom the way the decision does: the trade (the ticker, which way, how much),
/// then the person behind it and their reason, then where the vote stands, then the ballot. The
/// corner holds the one thing about time that matters — how long the vote has left, or, once it
/// has closed, the single status chip.
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
    /// The member reading. Their own ballot is left out of the faces, because it has its own line.
    var viewerId: String?
    /// False for the detail header, which sits directly on the canvas.
    var showsChrome = true
    /// Pulses an ink stroke once, for a card that just arrived.
    var highlight = false

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var pulse = false

    init(
        proposal: ProposalDTO,
        isVoting: Bool = false,
        onVote: @escaping (ProposalVoteChoice) -> Void = { _ in },
        destination: (() -> Destination)?,
        thesisIdentifierPrefix: String = "proposal-card-reason",
        viewerChoice: String? = nil,
        viewerId: String? = nil,
        showsChrome: Bool = true,
        highlight: Bool = false
    ) {
        self.proposal = proposal
        self.isVoting = isVoting
        self.onVote = onVote
        self.destination = destination
        self.thesisIdentifierPrefix = thesisIdentifierPrefix
        self.viewerChoice = viewerChoice
        self.viewerId = viewerId
        self.showsChrome = showsChrome
        self.highlight = highlight
    }

    /// At the accessibility sizes the corner and the comment count drop under what they sat
    /// beside, instead of squeezing the ticker or the tally to an ellipsis.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    /// The ring around each face has to be the colour behind the card, or the overlap shows.
    private var faceRing: Color { showsChrome ? MonacoTheme.surface : MonacoTheme.canvas }

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
            // A card, because it is the thing the member acts on: the ballot is cast right
            // here. A hairline at rest; the ink stroke is the pulse for a card that just landed.
            if showsChrome {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .strokeBorder(pulse ? MonacoTheme.ink : MonacoTheme.hairline, lineWidth: 1)
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
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                header
                amount
                premiumNudge
            }
            byline
            standing
        }
    }

    @ViewBuilder
    private var header: some View {
        if isStacked {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
                    mark
                    titles
                }
                corner
            }
        } else {
            HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
                mark
                titles
                Spacer(minLength: MonacoTheme.Space.s)
                corner
            }
        }
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(ProposalFeedCopy.title(for: proposal))
                .font(proposal.isTrade ? MonacoTheme.Typo.ticker : MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(1)
            Text(ProposalFeedCopy.subtitle(for: proposal))
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(isStacked ? 3 : 2)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// How long the vote has left while it is open, in the market's voice; the one status chip
    /// once it has closed. Never both, and never the chip on an open vote.
    @ViewBuilder
    private var corner: some View {
        switch ProposalCardCorner.of(proposal) {
        case .chip(let label):
            ProposalStatusChip(
                label: label,
                stage: ProposalExecutionStage.of(proposal),
                status: proposal.status,
                kind: proposal.resolvedKind
            )
        case .countdown(let label, let closesSoon):
            Text(label)
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(closesSoon ? MonacoTheme.warning : MonacoTheme.tertiaryText)
                .lineLimit(1)
        case .none:
            EmptyView()
        }
    }

    @ViewBuilder
    private var mark: some View {
        if proposal.isTrade {
            StockMark(symbol: proposal.symbol, size: 40)
        } else {
            // A coin says "stock"; a bot is a thing in the app, so it takes the sunken disc.
            SunkenGlyphMark(systemImage: "cpu", size: 40)
        }
    }

    @ViewBuilder
    private var premiumNudge: some View {
        if let bps = proposal.premiumBps,
           PreIpoCopy.showsPremiumNudge(premiumBps: bps, assetKind: proposal.resolvedAssetKind) {
            Text(PreIpoCopy.tradingPremiumNudge(bps: bps))
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    @ViewBuilder
    private var amount: some View {
        Group {
            switch proposal.resolvedKind {
            case "sell":
                Text(
                    ProposalShareFormatter.sharesLabel(
                        fromAtomics: proposal.tokenAmount ?? "0",
                        decimals: proposal.resolvedTokenDecimals,
                        kind: proposal.resolvedAssetKind
                    )
                )
                    .moneyFont(.large)
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
        // Its own height, always. The amount is the one line on the card that can shrink, so in
        // the feed at the accessibility sizes, where the card's summary was offered less height
        // than it needs, it alone gave way and sat at its minimum scale under a ticker twice its
        // size. Money may narrow to fit the width; it is never squeezed for height.
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityIdentifier("proposal-card-amount-\(proposal.id)")
    }

    /// The person behind it: their name, strong, with when they proposed it in the market's
    /// voice, then their reason, quiet. The detail screen quotes the full reason under the
    /// header, so there the header only names who proposed it.
    @ViewBuilder
    private var byline: some View {
        let name = proposal.proposerName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let thesis = proposal.thesis?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !thesis.isEmpty, showsChrome {
            VStack(alignment: .leading, spacing: 2) {
                if !name.isEmpty {
                    authorLine(
                        Text(name)
                            .font(MonacoTheme.Typo.calloutStrong)
                            .foregroundStyle(MonacoTheme.ink)
                    )
                }
                // Two-line excerpt; the full reason lives on the detail screen.
                Text(thesis)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("\(thesisIdentifierPrefix)-\(proposal.id)")
            }
        } else if !name.isEmpty {
            authorLine(
                Text(ProposalFeedCopy.proposedBy(name))
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
            )
        }
    }

    /// Who, then when. Side by side at the usual sizes, the name giving way before the stamp;
    /// one run of text at the accessibility sizes, so a name that wraps is followed by its stamp
    /// instead of having it parked beside its first line.
    @ViewBuilder
    private func authorLine(_ who: Text) -> some View {
        let age = proposal.createdAt.map { RelativeTimeFormatter.label(iso: $0) } ?? ""
        if isStacked {
            ProposalStampedLine.text(who, stamp: age)
                .fixedSize(horizontal: false, vertical: true)
        } else {
            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                who
                    .lineLimit(1)
                if !age.isEmpty {
                    Text(age)
                        .font(MonacoTheme.Typo.stamp)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                        .fixedSize()
                }
            }
            .accessibilityElement(children: .combine)
        }
    }

    /// Where the vote stands: the tally and the conversation count, then — when the payload
    /// names the ballots — the faces of the members already behind it.
    @ViewBuilder
    private var standing: some View {
        let yesVotes = ProposalYesVoters.votes(in: proposal.votes ?? [], excluding: viewerId)
        let yesNames = yesVotes.map(\.displayName)
        let comments = proposal.commentCount ?? 0
        if proposal.voteSummary != nil || comments > 0 || !yesNames.isEmpty {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                if proposal.voteSummary != nil || comments > 0 {
                    tallyLine(comments: comments)
                }
                if let sentence = ProposalYesVoters.sentence(yesNames) {
                    HStack(spacing: MonacoTheme.Space.s) {
                        BallotFaces(votes: yesVotes, ring: faceRing)
                        Text(sentence)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                            .lineLimit(isStacked ? 3 : 1)
                            .minimumScaleFactor(isStacked ? 1 : 0.85)
                    }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(sentence)
                    .accessibilityIdentifier("proposal-card-yes-voters-\(proposal.id)")
                }
            }
        }
    }

    @ViewBuilder
    private func tallyLine(comments: Int) -> some View {
        let tally = proposal.voteSummary.map {
            ProposalVoteTally(progress: ProposalVoteProgress(summary: $0), isOpen: proposal.isOpen)
                .accessibilityIdentifier("proposal-card-votes-\(proposal.id)")
        }
        if isStacked {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                tally
                if comments > 0 { commentCount(comments) }
            }
        } else {
            HStack(spacing: MonacoTheme.Space.s) {
                tally
                Spacer(minLength: MonacoTheme.Space.s)
                if comments > 0 { commentCount(comments) }
            }
        }
    }

    private func commentCount(_ count: Int) -> some View {
        Label("\(count)", systemImage: "bubble.left")
            .font(MonacoTheme.Typo.stamp)
            .foregroundStyle(MonacoTheme.muted)
            .labelStyle(ProposalFooterLabelStyle())
            .accessibilityLabel(ProposalFeedCopy.commentCount(count))
            .accessibilityIdentifier("proposal-card-comment-count-\(proposal.id)")
    }

    // MARK: Actions

    @ViewBuilder
    private var actions: some View {
        if let viewerChoice {
            // A ballot that is in reads as an entry in the ledger, under a rule — not as a
            // button that stopped working. Same neutral ink either way: nothing went up or down.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                MonacoRule()
                HStack(spacing: MonacoTheme.Space.s) {
                    Image(systemName: viewerChoice.lowercased() == "no" ? "xmark" : "checkmark")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(MonacoTheme.onBrand)
                        .frame(width: 22, height: 22)
                        .background(Circle().fill(MonacoTheme.brandFill))
                        .accessibilityHidden(true)
                    Text(ProposalFeedCopy.viewerVoted(viewerChoice))
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.ink)
                }
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
        viewerId: String? = nil,
        showsChrome: Bool = true
    ) {
        self.init(
            proposal: proposal,
            isVoting: isVoting,
            onVote: onVote,
            destination: nil,
            viewerChoice: viewerChoice,
            viewerId: viewerId,
            showsChrome: showsChrome
        )
    }
}

/// A name followed by its stamp as one run of text, for the sizes where the name wraps.
enum ProposalStampedLine {
    static func text(_ who: Text, stamp: String) -> Text {
        guard !stamp.isEmpty else { return who }
        return who
            + Text("  ")
            + Text(stamp)
                .font(MonacoTheme.Typo.stamp)
                .foregroundStyle(MonacoTheme.tertiaryText)
    }
}

/// What the card's top-right corner says: how long the vote has left while it is open, the one
/// status chip once it has closed.
enum ProposalCardCorner: Equatable {
    case chip(String)
    /// "Closes in 20h"; amber in the last hour.
    case countdown(String, closesSoon: Bool)
    case none

    static func of(_ proposal: ProposalDTO, now: Date = Date()) -> ProposalCardCorner {
        if let closed = ProposalFeedCopy.closedLabel(for: proposal) {
            return .chip(closed)
        }
        guard proposal.isOpen,
              let expiresAt = proposal.expiresAt,
              let label = ProposalTimeFormatter.closesLabel(expiresAt: expiresAt, now: now)
        else { return .none }
        return .countdown(label, closesSoon: ProposalTimeFormatter.closesSoon(expiresAt: expiresAt, now: now))
    }
}

/// Who is already behind a proposal, in words: "Ada and Ben voted yes".
///
/// First names, the way friends talk about each other — unless two of the voters share one,
/// when both keep their surnames so the sentence still says who.
enum ProposalYesVoters {
    /// Names the sentence lists before it counts the rest.
    static let namedLimit = 3

    /// The yes ballots in the order the server sent them. The viewer's own is left out when
    /// they are known: their vote has its own line on the card, and "You voted yes" twice on
    /// one screen reads as a mistake.
    static func names(in votes: [ProposalVoteDTO], excluding viewerId: String?) -> [String] {
        self.votes(in: votes, excluding: viewerId).map(\.displayName)
    }

    /// The same ballots with their ids, which is what picks each voter's face.
    static func votes(in votes: [ProposalVoteDTO], excluding viewerId: String?) -> [ProposalVoteDTO] {
        votes
            .filter { $0.choice.lowercased() == ProposalVoteChoice.yes.rawValue }
            .filter { vote in viewerId.map { vote.voterId != $0 } ?? true }
            .map { vote in
                ProposalVoteDTO(
                    voterId: vote.voterId,
                    displayName: vote.displayName.trimmingCharacters(in: .whitespacesAndNewlines),
                    choice: vote.choice,
                    castAt: vote.castAt
                )
            }
            .filter { !$0.displayName.isEmpty }
    }

    /// "Ada voted yes", "Ada and Ben voted yes", "Ada, Ben and Cy voted yes",
    /// "Ada, Ben, Cy and 2 others voted yes". Nil when nobody has.
    static func sentence(_ names: [String]) -> String? {
        guard !names.isEmpty else { return nil }
        let short = shortNames(names)
        var parts = Array(short.prefix(namedLimit))
        let rest = short.count - parts.count
        if rest > 0 {
            parts.append(rest == 1 ? "1 other" : "\(rest) others")
        }
        let list: String
        switch parts.count {
        case 1:
            list = parts[0]
        case 2:
            list = "\(parts[0]) and \(parts[1])"
        default:
            list = parts.dropLast().joined(separator: ", ") + " and " + parts[parts.count - 1]
        }
        return "\(list) voted yes"
    }

    /// Each name's first word, or the whole name where a first word is shared.
    static func shortNames(_ names: [String]) -> [String] {
        let firsts = names.map { name in
            name.split(whereSeparator: \.isWhitespace).first.map(String.init) ?? name
        }
        var counts: [String: Int] = [:]
        for first in firsts {
            counts[first.lowercased(), default: 0] += 1
        }
        return zip(names, firsts).map { full, first in
            (counts[first.lowercased()] ?? 0) > 1 ? full : first
        }
    }
}

/// Up to four faces of the members behind a proposal, overlapped the way a group avatar row is.
/// Decoration: the sentence beside it says the names, and VoiceOver reads that instead.
///
/// Each face is the voter's pixel animal, the same one their id picks everywhere else, cut
/// out of the one it overlaps by a ring in the colour behind the row. These were initials on
/// a sunken disc until the animals arrived, and a row of letters beside a row of animals on
/// the same screen read as two different apps.
struct BallotFaces: View {
    let votes: [ProposalVoteDTO]
    /// The colour behind the row, so each face is cut out of the one it overlaps.
    var ring: Color = MonacoTheme.surface
    var size: CGFloat = 24

    static let visibleLimit = 4

    var body: some View {
        HStack(spacing: -size / 4) {
            ForEach(Array(votes.prefix(Self.visibleLimit).enumerated()), id: \.offset) { _, vote in
                Image(PixelAnimal.forSeed(vote.voterId.isEmpty ? vote.displayName : vote.voterId).imageName)
                    .resizable()
                    .interpolation(.none)
                    .scaledToFill()
                    .frame(width: size, height: size)
                    .clipShape(Circle())
                    .overlay(Circle().strokeBorder(ring, lineWidth: 2))
            }
        }
        .accessibilityHidden(true)
    }
}

/// Dots (up to 12 voters) and "2 of 5 voted · 3 yes to pass". Brand = yes, loss outline = no,
/// hairline = still to vote. Green stays reserved for profit.
struct ProposalVoteTally: View {
    let progress: ProposalVoteProgress
    var isOpen = true

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

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
                // A ballot landing fills its dot; it does not bounce.
                .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: dots)
                .accessibilityHidden(true)
            }
            Text(caption)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .lineLimit(dynamicTypeSize.isAccessibilitySize ? 3 : 1)
                .minimumScaleFactor(dynamicTypeSize.isAccessibilitySize ? 1 : 0.85)
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
