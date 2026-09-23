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
    /// The viewer's own cabals, for the resolved tint. Optional because the debug harnesses and
    /// the previews render this card with no session behind it.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?
    @State private var pulse = false
    /// The ballot this member just cast, while it is in flight. The pill they tapped keeps its
    /// full strength and the other one falls back, so the card says which way they voted before
    /// the server has answered.
    @State private var pendingChoice: ProposalVoteChoice?
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

    /// The proposing cabal's tint: the one the surface declared, else the resolved tint for the
    /// cabal this proposal belongs to. Nil when the payload carries no group and nobody told us —
    /// the card then ships without a rail rather than inventing a colour for a cabal it cannot
    /// name.
    ///
    /// The surface is asked first and it wins. A cross-cabal surface resolves the viewer's cabals
    /// once and declares the colour it resolved; a card that re-hashed the id underneath it would
    /// paint the same cabal two colours on two screens, which is the one thing a tint may not do.
    /// Everything else goes through `CabalTintAssignment`, never `forGroupId` directly, so the
    /// rail here and the mark on the Cabals tab agree. `cabal:` carries the mark and the name; it
    /// does not carry the colour.
    private var tint: MonacoTheme.CabalTint? {
        if let environmentTint { return environmentTint }
        let groupId = cabal?.id ?? proposal.groupId ?? ""
        guard !groupId.isEmpty else { return nil }
        return ProposalCabalTint.tint(forGroupId: groupId, in: session)
    }

    private var progress: ProposalVoteProgress? {
        proposal.voteSummary.map(ProposalVoteProgress.init(summary:))
    }

    private var radius: CGFloat { MonacoTheme.Radius.container }

    private var shape: RoundedRectangle {
        RoundedRectangle(cornerRadius: radius, style: .continuous)
    }

    /// The last hour of a vote is a fact about the clock, not about the payload. Without a tick a
    /// card already on screen keeps its grey "Closes in 4h" footnote through the whole final hour
    /// and never reaches the countdown, because nothing re-reads a list nobody is scrolling. A
    /// minute is the resolution the header needs; the countdown capsule runs its own seconds
    /// inside it. A settled proposal has no deadline left to watch and does not pay for a timeline.
    var body: some View {
        if proposal.isOpen, proposal.expiresAt != nil {
            TimelineView(.periodic(from: .now, by: 60)) { context in
                card(now: context.date)
            }
        } else {
            card(now: Date())
        }
    }

    private func card(now: Date) -> some View {
        VStack(alignment: .leading, spacing: compact ? MonacoTheme.Space.sm : MonacoTheme.Space.m) {
            if let destination {
                NavigationLink {
                    destination()
                } label: {
                    summary(now: now)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("proposal-card-open-\(proposal.id)")
            } else {
                summary(now: now)
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
                    .strokeBorder(edgeTint(now: now), lineWidth: 1)
                    .opacity(edgeOpacity(now: now))
            }
        }
        // The cabal's own colour washes the card once as the vote lands. Never green: the cabal
        // agreed to spend money, nobody has made any.
        .overlay {
            if showsChrome, passCelebration, let tint {
                shape.fill(tint.soft).allowsHitTesting(false)
            }
        }
        .scaleEffect(passCelebration ? 1.04 : 1)
        .task(id: highlight) {
            guard highlight, !reduceMotion else { return }
            withAnimation(MonacoMotion.glide) { pulse = true }
            try? await Task.sleep(for: .seconds(1))
            withAnimation(.easeIn(duration: 0.4)) { pulse = false }
        }
        // The ballot has landed, or been refused: the pills go back to sharing the row evenly.
        .onChange(of: isVoting) { _, voting in
            guard !voting else { return }
            pendingChoice = nil
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
    private func edgeTint(now: Date) -> Color {
        closesSoon(now: now) ? MonacoTheme.warning : MonacoTheme.fgPrimary
    }

    private func edgeOpacity(now: Date) -> Double {
        if pulse { return 1 }
        return closesSoon(now: now) ? 1 : 0
    }

    private func closesSoon(now: Date) -> Bool {
        guard proposal.isOpen, let expiresAt = proposal.expiresAt else { return false }
        return ProposalTimeFormatter.closesSoon(expiresAt: expiresAt, now: now)
    }

    // MARK: Summary

    private func summary(now: Date) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            if !compact {
                proposerLine(now: now)
            }
            header(now: now)
            if !compact {
                reason
            }
            tally
            if !compact {
                footer(now: now)
            }
        }
    }

    /// Who wants this, which cabal they want it in, and when they said so.
    ///
    /// The proposer had no face anywhere in the product before v3 and their name was the first
    /// words of their own thesis, in bold, which read like a quotation attribution rather than a
    /// person. `MonacoAvatar` renders initials until `profilePhotoUrl` reaches the proposal
    /// payload; nothing here invents a photo.
    ///
    /// At an accessibility text size the row stops being a row. Four single-line items sharing one
    /// line means the name — the headline of this whole card — truncates to a glyph or two, so the
    /// face and the name take the first line on their own and the cabal and the age fall to a
    /// second as one wrapping sentence rather than two labels competing for the same 80pt.
    @ViewBuilder
    private func proposerLine(now: Date) -> some View {
        let name = proposal.proposerName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let age = proposal.createdAt.map { RelativeTimeFormatter.label(iso: $0, now: now) } ?? ""
        if !name.isEmpty || cabal != nil {
            if dynamicTypeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    if !name.isEmpty {
                        HStack(alignment: .top, spacing: MonacoTheme.Space.s) {
                            MonacoAvatar(photoURL: nil, displayName: name, size: 28)
                            Text(name)
                                .font(MonacoTheme.Typo.callout.weight(.semibold))
                                .foregroundStyle(MonacoTheme.fgPrimary)
                                .fixedSize(horizontal: false, vertical: true)
                            Spacer(minLength: 0)
                        }
                    }
                    if let secondary = secondaryProposerLine(age: age) {
                        HStack(alignment: .top, spacing: MonacoTheme.Space.s) {
                            if let cabal {
                                CabalMark(groupId: cabal.id, name: cabal.name, size: 16)
                            }
                            Text(secondary)
                                .font(MonacoTheme.Typo.caption)
                                .foregroundStyle(MonacoTheme.fgMuted)
                                .fixedSize(horizontal: false, vertical: true)
                            Spacer(minLength: 0)
                        }
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
            } else {
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
                    if !age.isEmpty {
                        Text(age)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.fgSubtle)
                            .lineLimit(1)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
    }

    /// The cabal and the age as one wrapping sentence, for the accessibility-size layout. Nil when
    /// the payload names neither, in which case there is no second line to draw.
    private func secondaryProposerLine(age: String) -> String? {
        let parts = [cabal?.name, age].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// The object: the stock, what would happen to it, and for how much.
    ///
    /// At an accessibility text size the figure comes out of the row and goes under the title: an
    /// 18pt expanded title and a 28pt figure cannot share one line without both sitting on their
    /// `minimumScaleFactor` floors, and what is being voted on is the thing that has to be read.
    @ViewBuilder
    private func header(now: Date) -> some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
                    mark
                    titleBlock
                    Spacer(minLength: 0)
                }
                amount
                // The chip's home at this size. The compact card has no footer to fall back to —
                // it is the inline card in a chat thread — so a closed proposal has to say
                // "Bought" here or it does not say it at all.
                state(now: now)
            }
        } else {
            HStack(alignment: .center, spacing: MonacoTheme.Space.sm) {
                mark
                titleBlock
                Spacer(minLength: MonacoTheme.Space.s)
                trailingHeader(now: now)
            }
        }
    }

    /// What is being proposed, over the one-line summary of it. Scaled to fit while it shares a
    /// row with the figure; allowed to wrap once it has the row to itself.
    private var titleBlock: some View {
        let isAccessibilitySize = dynamicTypeSize.isAccessibilitySize
        return VStack(alignment: .leading, spacing: 2) {
            Text(ProposalFeedCopy.title(for: proposal))
                .displayFont(.section)
                .foregroundStyle(MonacoTheme.fgPrimary)
                .lineLimit(isAccessibilitySize ? 3 : 1)
                .minimumScaleFactor(isAccessibilitySize ? 1 : 0.8)
                .fixedSize(horizontal: false, vertical: true)
            Text(ProposalFeedCopy.subtitle(for: proposal))
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.fgMuted)
                .lineLimit(isAccessibilitySize ? 3 : 1)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// The amount stacked over the state chip, at the trailing edge of the header row.
    private func trailingHeader(now: Date) -> some View {
        VStack(alignment: .trailing, spacing: 4) {
            amount
            state(now: now)
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
    private func state(now: Date) -> some View {
        if proposal.isOpen, closesSoon(now: now), let expiresAt = proposal.expiresAt {
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

    private func footer(now: Date) -> some View {
        HStack(spacing: MonacoTheme.Space.m) {
            if proposal.isOpen, !closesSoon(now: now), let expiresAt = proposal.expiresAt,
               let closes = ProposalTimeFormatter.closesLabel(expiresAt: expiresAt, now: now) {
                Label(closes, systemImage: "clock")
                    .foregroundStyle(MonacoTheme.fgMuted)
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
                    cast(.yes)
                } label: {
                    Text(ProposalFeedCopy.voteYes)
                }
                .buttonStyle(.monacoPrimary)
                .opacity(pillOpacity(for: .yes))
                .accessibilityIdentifier("proposal-card-vote-yes-\(proposal.id)")

                Button {
                    cast(.no)
                } label: {
                    Text(ProposalFeedCopy.voteNo)
                }
                .buttonStyle(.monacoSecondary)
                .opacity(pillOpacity(for: .no))
                .accessibilityIdentifier("proposal-card-vote-no-\(proposal.id)")
            }
            .monacoFullWidthButtons()
            .disabled(isVoting)
            .animation(MonacoMotion.snap.reduced(reduceMotion), value: pendingChoice)
            .animation(MonacoMotion.snap.reduced(reduceMotion), value: isVoting)
            .transition(.opacity)
        }
    }

    private func cast(_ choice: ProposalVoteChoice) {
        Haptics.tap()
        pendingChoice = choice
        onVote(choice)
    }

    /// While a ballot is in flight the pill the member tapped holds its full strength and the
    /// other one falls back, so the card says which way they voted in the moment they voted rather
    /// than greying out both and saying only "wait".
    private func pillOpacity(for choice: ProposalVoteChoice) -> Double {
        guard isVoting else { return 1 }
        guard let pendingChoice else { return 0.6 }
        return pendingChoice == choice ? 1 : 0.35
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
            let ballots = proposal.isOpen ? dots : dots.filter { $0 != .pending }
            return ballots.enumerated().map { MonacoVote(id: "ballot-\($0.offset)", state: $0.element) }
        }

        let cast = named.prefix(progress.eligibleCount).map { vote in
            MonacoVote(
                id: vote.voterId,
                state: vote.choice.lowercased() == "yes" ? .yes : .no,
                face: MonacoFace(id: vote.voterId, displayName: vote.displayName)
            )
        }
        // A settled proposal shows the ballots that were cast and nothing else. An empty slot on
        // a closed vote reads as a member who still owes one, and nobody owes anything now.
        guard proposal.isOpen else { return cast }
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
