import MonacoCore
import SwiftUI

/// One proposal: who wants to spend the cabal's money on what, why, where the vote stands, and
/// Yes / No while the viewer may still vote. Read-only proposals (seeded, or the viewer is not in
/// the voter set) show the tally only.
///
/// Used by `ProposalFeedView`, by Group detail (summary links to detail), as the header of
/// `ProposalDetailView` (no link, no card chrome) and — in `compact` form — inline in chat.
///
/// **v3.** The proposal is the most social object in the product and it used to render as a
/// monochrome row with two buttons and eight anonymous dots, with the proposer's name buried in
/// the first line of their own thesis. It now leads with a face and a name, carries the proposing
/// cabal's colour as a 3pt rail, quotes the thesis in that cabal's wash, and shows the tally as
/// faces that still say *how* each member voted (`MonacoVoteFace`). E2, because on a feed of
/// proposals the proposal is the screen.
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
    /// Pulses the card's edge once, for a proposal that just arrived.
    var highlight = false
    /// The chat card: the stock, the amount, the tally and live Yes / No, and nothing else.
    ///
    /// Chat already says who is talking and which cabal you are standing in, so the compact card
    /// drops the proposer line, the thesis and the footer rather than repeating them one bubble
    /// below. Owned here so the inline card in `GroupChatView` and the card in the feed cannot
    /// drift into two different proposals.
    var compact = false
    /// The proposing cabal, when the surface the card sits on is not already that cabal's.
    ///
    /// The feed and the cabal screen are a single cabal's, so they leave this nil and let the
    /// tint rail carry the identity. A cross-cabal surface passes it and gets the mark and name.
    var cabal: ProposalCardCabal?

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.cabalTint) private var environmentTint
    @State private var pulse = false
    /// The tally has crossed the pass line while this card was on screen. The one `celebrate` in
    /// the app (§4 #14) — a second call site means one of them is wrong.
    @State private var passCelebration = false

    init(
        proposal: ProposalDTO,
        isVoting: Bool = false,
        onVote: @escaping (ProposalVoteChoice) -> Void = { _ in },
        destination: (() -> Destination)?,
        thesisIdentifierPrefix: String = "proposal-card-reason",
        viewerChoice: String? = nil,
        showsChrome: Bool = true,
        highlight: Bool = false,
        compact: Bool = false,
        cabal: ProposalCardCabal? = nil
    ) {
        self.proposal = proposal
        self.isVoting = isVoting
        self.onVote = onVote
        self.destination = destination
        self.thesisIdentifierPrefix = thesisIdentifierPrefix
        self.viewerChoice = viewerChoice
        self.showsChrome = showsChrome
        self.highlight = highlight
        self.compact = compact
        self.cabal = cabal
    }

    // MARK: Identity

    /// The proposing cabal's tint: the one the surface set, else the one the proposal's own group
    /// id hashes to. Nil when the payload carries no group and nobody told us — the card then
    /// ships without a rail rather than inventing a colour for a cabal it cannot name.
    private var tint: MonacoTheme.CabalTint? {
        if let cabal { return .forGroupId(cabal.id) }
        if let environmentTint { return environmentTint }
        guard let groupId = proposal.groupId, !groupId.isEmpty else { return nil }
        return .forGroupId(groupId)
    }

    private var progress: ProposalVoteProgress? {
        proposal.voteSummary.map(ProposalVoteProgress.init(summary:))
    }

    private var radius: CGFloat { MonacoTheme.Radius.container }

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: compact ? MonacoTheme.Space.sm : MonacoTheme.Space.m) {
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
        .overlay(alignment: .leading) { rail }
        .clipShape(showsChrome ? AnyShape(shape) : AnyShape(Rectangle()))
        .modifier(ProposalCardSurface(isVisible: showsChrome, compact: compact, radius: radius))
        .overlay {
            if showsChrome {
                shape
                    .strokeBorder(edgeTint, lineWidth: 1)
                    .opacity(edgeOpacity)
            }
        }
        .scaleEffect(passCelebration ? 1.04 : 1)
        .task(id: highlight) {
            guard highlight, !reduceMotion else { return }
            withAnimation(MonacoMotion.glide) { pulse = true }
            try? await Task.sleep(for: .seconds(1))
            withAnimation(.easeIn(duration: 0.4)) { pulse = false }
        }
        // The one celebration in the app: this vote is the one that took the proposal over the
        // line. Nothing celebrates a proposal that was already passing when the card appeared, so
        // the trigger is the crossing, not the state.
        .onChange(of: hasReachedThreshold) { wasPassing, isPassing in
            guard isPassing, !wasPassing, !reduceMotion else { return }
            Task {
                withAnimation(MonacoMotion.celebrate) { passCelebration = true }
                try? await Task.sleep(for: .milliseconds(260))
                withAnimation(MonacoMotion.settle) { passCelebration = false }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("proposal-card-\(proposal.id)")
    }

    /// Yes votes are at or past the number needed. Only meaningful while the vote is open.
    private var hasReachedThreshold: Bool {
        guard proposal.isOpen, let progress, progress.eligibleCount > 0 else { return false }
        return progress.yesCount >= progress.yesNeeded
    }

    /// The 3pt leading rail in the proposing cabal's colour, so a feed of proposals says which
    /// cabal is arguing before a word is read.
    @ViewBuilder
    private var rail: some View {
        if showsChrome, let tint {
            Rectangle()
                .fill(tint.fill)
                .frame(width: 3)
                .accessibilityHidden(true)
        }
    }

    /// The card's edge: `warning` while the vote closes within the hour, the pulse colour for a
    /// proposal that just arrived, and otherwise nothing — `monacoElevation` owns the resting edge.
    private var edgeTint: Color {
        closesSoon ? MonacoTheme.warning : MonacoTheme.fgPrimary
    }

    private var edgeOpacity: Double {
        if pulse { return 1 }
        return closesSoon ? 1 : 0
    }

    private var closesSoon: Bool {
        guard proposal.isOpen, let expiresAt = proposal.expiresAt else { return false }
        return ProposalTimeFormatter.closesSoon(expiresAt: expiresAt)
    }

    // MARK: Summary

    private var summary: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            if !compact {
                proposerLine
            }
            header
            if !compact {
                reason
            }
            tally
            if !compact {
                footer
            }
        }
    }

    /// Who wants this, which cabal they want it in, and when they said so.
    ///
    /// The proposer had no face anywhere in the product before v3 and their name was the first
    /// words of their own thesis, in bold, which read like a quotation attribution rather than a
    /// person. `MonacoAvatar` renders initials until `profilePhotoUrl` reaches the proposal
    /// payload; nothing here invents a photo.
    @ViewBuilder
    private var proposerLine: some View {
        let name = proposal.proposerName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !name.isEmpty || cabal != nil {
            HStack(spacing: MonacoTheme.Space.s) {
                if !name.isEmpty {
                    MonacoAvatar(photoURL: nil, displayName: name, size: 28)
                    Text(name)
                        .font(MonacoTheme.Typo.callout.weight(.semibold))
                        .foregroundStyle(MonacoTheme.fgPrimary)
                        .lineLimit(1)
                }
                if let cabal {
                    CabalMark(groupId: cabal.id, name: cabal.name, size: 16)
                    Text(cabal.name)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.fgMuted)
                        .lineLimit(1)
                }
                Spacer(minLength: MonacoTheme.Space.s)
                if let createdAt = proposal.createdAt {
                    Text(RelativeTimeFormatter.label(iso: createdAt))
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.fgSubtle)
                        .lineLimit(1)
                }
            }
            .accessibilityElement(children: .combine)
        }
    }

    /// The object: the stock, what would happen to it, and for how much.
    private var header: some View {
        HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
            mark
            VStack(alignment: .leading, spacing: 2) {
                Text(ProposalFeedCopy.title(for: proposal))
                    .displayFont(.section)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                Text(ProposalFeedCopy.subtitle(for: proposal))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .lineLimit(1)
            }
            Spacer(minLength: MonacoTheme.Space.s)
            trailingHeader
        }
    }

    /// The amount stacked over the state chip, or — at an accessibility text size, where two
    /// columns of figures stop fitting side by side — the amount alone, with the chip on its own
    /// line below the tally. A 56pt figure and a capsule cannot share 120pt of width.
    private var trailingHeader: some View {
        VStack(alignment: .trailing, spacing: 4) {
            amount
            if !dynamicTypeSize.isAccessibilitySize {
                state
            }
        }
    }

    @ViewBuilder
    private var mark: some View {
        if proposal.isTrade {
            StockMark(symbol: proposal.symbol, size: compact ? 36 : 44)
        } else {
            StockMark(systemImage: "cpu", size: compact ? 36 : 44)
        }
    }

    /// The closing-soon countdown, the closed-state chip, or nothing while a vote is quietly open.
    @ViewBuilder
    private var state: some View {
        if proposal.isOpen, closesSoon, let expiresAt = proposal.expiresAt {
            ProposalClosingSoonChip(expiresAt: expiresAt)
        } else if let closed = ProposalFeedCopy.closedLabel(for: proposal) {
            ProposalStatusChip(
                label: closed,
                stage: ProposalExecutionStage.of(proposal),
                status: proposal.status,
                kind: proposal.resolvedKind
            )
        }
    }

    @ViewBuilder
    private var amount: some View {
        Group {
            switch proposal.resolvedKind {
            case "sell":
                Text(ProposalShareFormatter.sharesLabel(fromAtomics: proposal.tokenAmount ?? "0"))
                    .moneyFont(.large)
                    .foregroundStyle(MonacoTheme.fgPrimary)
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

    /// The proposer's case, quoted in the cabal's own wash.
    ///
    /// The detail screen quotes the full reason below the header, so there the header only names
    /// the proposer — who now has a face and a line of their own, which is why the name has come
    /// out of the front of their sentence.
    @ViewBuilder
    private var reason: some View {
        let thesis = proposal.thesis?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !thesis.isEmpty, showsChrome {
            ProposalQuoteBlock(text: thesis, lineLimit: 2, tint: tint)
                .accessibilityIdentifier("\(thesisIdentifierPrefix)-\(proposal.id)")
        }
    }

    /// Faces when the ballots can be named and there are few enough of them, dots otherwise, and
    /// the caption either way — it is the accessible name for the whole element.
    @ViewBuilder
    private var tally: some View {
        if let progress {
            ProposalVoteTally(
                progress: progress,
                isOpen: proposal.isOpen,
                votes: ProposalVoteFaces.votes(for: proposal, progress: progress)
            )
            .accessibilityIdentifier("proposal-card-votes-\(proposal.id)")
        }
    }

    private var footer: some View {
        HStack(spacing: MonacoTheme.Space.m) {
            if proposal.isOpen, !closesSoon, let expiresAt = proposal.expiresAt,
               let closes = ProposalTimeFormatter.closesLabel(expiresAt: expiresAt) {
                Label(closes, systemImage: "clock")
                    .foregroundStyle(MonacoTheme.fgMuted)
            }
            if dynamicTypeSize.isAccessibilitySize {
                state
            }
            Spacer(minLength: 0)
            if let count = proposal.commentCount, count > 0 {
                Label("\(count)", systemImage: "bubble.left")
                    .foregroundStyle(MonacoTheme.fgMuted)
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
            ProposalViewerBallot(choice: viewerChoice)
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
        showsChrome: Bool = true,
        compact: Bool = false,
        cabal: ProposalCardCabal? = nil
    ) {
        self.init(
            proposal: proposal,
            isVoting: isVoting,
            onVote: onVote,
            destination: nil,
            viewerChoice: viewerChoice,
            showsChrome: showsChrome,
            compact: compact,
            cabal: cabal
        )
    }
}

/// The proposing cabal, for a card on a surface that is not already that cabal's.
struct ProposalCardCabal: Equatable {
    let id: String
    let name: String

    init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

/// E2 in the feed, E1 inline in chat, nothing at all for the detail header.
///
/// A proposal outranks everything around it on a feed of proposals, so it takes the raised step.
/// Inline in a chat thread it is one message among many and takes the card step, or the thread
/// would read as a column of elevated slabs.
private struct ProposalCardSurface: ViewModifier {
    let isVisible: Bool
    let compact: Bool
    let radius: CGFloat

    func body(content: Content) -> some View {
        if isVisible {
            content.monacoElevation(compact ? .card : .raised, radius: radius)
        } else {
            content
        }
    }
}

/// "You voted yes", with the same ring that marks the ballot in the tally above it.
private struct ProposalViewerBallot: View {
    let choice: String

    private var isNo: Bool { choice.lowercased() == "no" }

    private var ring: Color { isNo ? MonacoTheme.loss : MonacoTheme.brand }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "checkmark")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(ring)
                .frame(width: 22, height: 22)
                .background(Circle().fill(ring.opacity(0.18)))
                .overlay { Circle().strokeBorder(ring, lineWidth: 2) }
            Text(ProposalFeedCopy.viewerVoted(choice))
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(MonacoTheme.fgPrimary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }
}

/// The last hour of a vote, counting down live.
///
/// A deadline is the one thing on a proposal card that changes while you look at it, and it used
/// to be a grey footnote saying "Closes in 1h" for fifty-nine of those sixty minutes. The capsule
/// is `warningWash` — amber, never red: nothing has gone wrong, it is about to be too late.
///
/// Under Reduce Motion the seconds stop and the chip states the minutes, because a digit changing
/// once a second on a card that is not being interacted with is exactly the motion that setting
/// asks to be turned off.
struct ProposalClosingSoonChip: View {
    let expiresAt: String

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Group {
            if reduceMotion {
                chip(label: staticLabel, accessible: staticLabel)
            } else {
                TimelineView(.periodic(from: .now, by: 1)) { context in
                    let remaining = ProposalCountdown.remaining(expiresAt: expiresAt, now: context.date)
                    chip(
                        label: ProposalCountdown.clock(remaining),
                        accessible: ProposalCountdown.spoken(remaining)
                    )
                }
            }
        }
        .accessibilityIdentifier("proposal-closing-soon")
    }

    private var staticLabel: String {
        ProposalTimeFormatter.closesLabel(expiresAt: expiresAt) ?? ProposalCountdown.closedLabel
    }

    private func chip(label: String, accessible: String) -> some View {
        HStack(spacing: 4) {
            Image(systemName: "clock")
                .font(.system(size: 10, weight: .bold))
            Text(label)
                .font(MonacoTheme.Typo.micro.monospacedDigit())
        }
        .foregroundStyle(MonacoTheme.warningOnWash)
        .padding(.horizontal, 10)
        .padding(.vertical, 5)
        .background(Capsule().fill(MonacoTheme.warningWash))
        .lineLimit(1)
        .fixedSize()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessible)
    }
}

/// The countdown's arithmetic and its two labels, pure so they are testable without a clock.
enum ProposalCountdown {
    static let closedLabel = "Voting closed"

    static func remaining(expiresAt: String, now: Date) -> Int {
        guard let expiry = ProposalTimeFormatter.parse(expiresAt) else { return 0 }
        return max(Int(expiry.timeIntervalSince(now)), 0)
    }

    /// "42:07" — minutes and seconds, because the chip only appears in the final hour.
    static func clock(_ remaining: Int) -> String {
        guard remaining > 0 else { return closedLabel }
        return String(format: "%d:%02d", remaining / 60, remaining % 60)
    }

    /// What VoiceOver says. A screen reader announcing "forty two colon zero seven" every second
    /// is not a countdown, it is a siren, so the spoken form is coarse on purpose.
    static func spoken(_ remaining: Int) -> String {
        guard remaining > 0 else { return closedLabel }
        let minutes = remaining / 60
        if minutes >= 1 { return "Closes in \(minutes) \(minutes == 1 ? "minute" : "minutes")" }
        return "Closes in under a minute"
    }
}

/// Ballots as faces, and "2 of 5 voted · 3 yes to pass".
///
/// `MonacoVoteFaceRow` draws the marks — a 22pt avatar ringed `brand` for yes and `loss` for no,
/// a dashed hollow circle for a member who has not voted, and the 8pt dots past eight voters or
/// at an accessibility text size. The caption is the accessible name for the whole element, which
/// is why the row itself is hidden from VoiceOver.
struct ProposalVoteTally: View {
    let progress: ProposalVoteProgress
    var isOpen = true
    /// One entry per eligible voter. Empty falls back to the caption alone.
    var votes: [MonacoVote] = []

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var caption: String {
        isOpen ? progress.caption : progress.closedCaption
    }

    var body: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            if !votes.isEmpty {
                MonacoVoteFaceRow(votes: votes)
            }
            Text(caption)
                .font(MonacoTheme.Typo.caption.monospacedDigit())
                .foregroundStyle(MonacoTheme.fgMuted)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
                .contentTransition(reduceMotion ? .identity : .numericText())
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(caption)
    }
}

/// Turns a proposal's ballots into the tally's marks, naming the voters it can and counting the
/// ones it cannot.
///
/// Detail payloads carry `votes` — `voterId`, `displayName`, `choice` — so those ballots get a
/// face. List rows carry only the summary, so their ballots are counted, not named: they render
/// as a filled ring with no initials, which still says how the vote was cast. Members who have
/// not voted are never named, because the payload does not say who they are.
///
/// **Nothing here invents a voter.** A pending slot is a slot, not a person.
enum ProposalVoteFaces {
    static func votes(for proposal: ProposalDTO, progress: ProposalVoteProgress) -> [MonacoVote] {
        // Past the dot cap the tally is a caption, exactly as it has always been: twelve marks
        // stop reading as a count of people and start reading as a texture.
        guard progress.eligibleCount > 0, progress.eligibleCount <= ProposalVoteProgress.maxDots else {
            return []
        }

        let named = proposal.votes ?? []
        if named.isEmpty {
            guard let dots = progress.dots else { return [] }
            return dots.enumerated().map { MonacoVote(id: "ballot-\($0.offset)", state: $0.element) }
        }

        let cast = named.prefix(progress.eligibleCount).map { vote in
            MonacoVote(
                id: vote.voterId,
                state: vote.choice.lowercased() == "yes" ? .yes : .no,
                face: MonacoFace(id: vote.voterId, displayName: vote.displayName)
            )
        }
        let pending = max(progress.eligibleCount - cast.count, 0)
        return cast + (0..<pending).map { MonacoVote(id: "pending-\($0)", state: .pending) }
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
