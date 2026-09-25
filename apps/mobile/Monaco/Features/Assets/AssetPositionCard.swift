import MonacoCore
import SwiftUI

/// "Your cabals' position": the card that stops this screen reading as a broker's.
///
/// A row per cabal that holds the stock — what it holds, what that is worth, what
/// the cabal has made on it, and how much of that is the member's own — then the
/// open votes, with the faces of the people who have already voted.
///
/// The totals and every sentence on this card come from `AssetPositionSummary`
/// (MonacoCore, tested). Nothing here does arithmetic.
struct AssetPositionCard: View {
    let summary: AssetPositionSummary
    /// The open votes themselves. They are not folded into the summary because a
    /// cabal with no holding but an open vote still belongs on this card: proposing
    /// to buy something you do not yet own is the normal case, not an edge one.
    let proposals: [AssetProposalDTO]
    let symbol: String
    /// Opens the cabal. Nil in a preview.
    var openCabal: ((String) -> Void)?
    /// Opens one proposal.
    var openProposal: ((AssetProposalDTO) -> Void)?

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// Every other card below the chart gives up its side-by-side layout at the
    /// accessibility sizes — the stats grid drops to one column, the Pyth legs
    /// stack. This one held on to its rows and truncated a member's money instead,
    /// which is the one thing on the screen that must never be cut short.
    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        AssetDetailCard(
            title: "Your cabals' position",
            identifier: "asset-detail-position"
        ) {
            VStack(alignment: .leading, spacing: 0) {
                totals
                    .padding(.bottom, MonacoTheme.Space.m)
                if !summary.holdings.isEmpty {
                    // The holdings are a ruled list under the totals: a rule across the
                    // section above the first row, then one under each row's text.
                    AssetCardDivider()
                    ForEach(Array(summary.holdings.enumerated()), id: \.element.id) { index, holding in
                        if index > 0 {
                            AssetCardDivider(leading: AssetCardDivider.inset(afterMark: HoldingRow.markSize))
                        }
                        HoldingRow(holding: holding, openCabal: openCabal)
                    }
                }
                if let notice = summary.unvaluedNotice {
                    // Never silence. A cabal we could not price is not a cabal that
                    // holds nothing, and the difference is someone's money.
                    Label(notice, systemImage: "exclamationmark.triangle")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.vertical, MonacoTheme.Space.s)
                        .accessibilityIdentifier("asset-position-unvalued")
                }
                if let voteHeadline = summary.voteHeadline {
                    AssetCardDivider()
                    votes(voteHeadline)
                }
            }
        }
    }

    // MARK: - Totals

    private var totals: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            Text(summary.headline)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)

            // With nothing held — a cabal voting on its first buy — the headline says
            // so, and a large "$0.00" over "Your slice of $0.00" would say it twice more.
            if !summary.holdings.isEmpty {
                // The member's own slice is the headline figure. The cabals' total is
                // context underneath it — they came to see their own money.
                //
                // The badge does *not* sit on this baseline. It measures what the cabals
                // made, the slice is what the member holds, and side by side they read as
                // one pair: "$837.32, up $323.83", when the member's share of that gain is
                // about a fifth of it. It gets its own line and its own possessive below.
                Text(UsdAmountFormatter.format(decimalString: summary.myTotalSliceUsd))
                    .moneyFont(.large)
                    .foregroundStyle(MonacoTheme.ink)

                Text(AssetPositionTotalsCopy.sliceLine(totalValueUsd: summary.totalValueUsd, holdings: summary.holdings))
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)

                if let pnlLabel = summary.totalPnlLabel {
                    cabalReturn(pnlLabel)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenTotals)
        .accessibilityIdentifier("asset-position-totals")
    }

    /// The cabals' P&L, named. Label and badge sit on one line until the text stops
    /// fitting beside it, the way every other pairing on this screen behaves.
    @ViewBuilder
    private func cabalReturn(_ label: String) -> some View {
        let name = Text(label)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(MonacoTheme.muted)
        let badge = PnLBadge(dollarPnl: summary.totalDollarPnl, percentReturn: summary.totalPercentReturn)
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: 4) {
                    name
                    badge
                }
            } else {
                HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                    name
                    Spacer(minLength: MonacoTheme.Space.s)
                    badge
                }
            }
        }
        .padding(.top, 2)
    }

    private var spokenTotals: String {
        guard !summary.holdings.isEmpty else { return "\(summary.headline)." }
        var sentence = "\(summary.headline). Your slice, "
        sentence += UsdAmountFormatter.format(decimalString: summary.myTotalSliceUsd)
        sentence += ", of \(UsdAmountFormatter.format(decimalString: summary.totalValueUsd)) held. "
        // The possessive is read out too: a figure that is not the listener's own must
        // not arrive unqualified on the one output where there is no layout to say so.
        if let pnlLabel = summary.totalPnlLabel {
            sentence += "\(pnlLabel), "
            sentence += PnLSpeech.badge(dollarPnl: summary.totalDollarPnl, percentReturn: summary.totalPercentReturn)
            sentence += "."
        }
        return sentence
    }

    // MARK: - Votes

    /// The open votes: a line saying how many, and one ruled row per vote.
    ///
    /// The count is set as a caption, the way the totals above open with "3 cabals hold
    /// AAPLx": in 17pt DemiBold it had the same weight as the vote titles under it, and the
    /// block read as five headlines in a row.
    private func votes(_ headline: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            let title = Text(headline)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
            let errand = summary.waitingOnYou.map { waiting in
                Text(waiting)
                    .font(MonacoTheme.Typo.captionStrong)
                    .foregroundStyle(MonacoTheme.brandOnWash)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Capsule().fill(MonacoTheme.brandWash))
                    .accessibilityIdentifier("asset-position-waiting-on-you")
            }
            Group {
                if isStacked {
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        title
                        errand
                    }
                } else {
                    HStack(alignment: .center, spacing: MonacoTheme.Space.s) {
                        title
                        Spacer(minLength: MonacoTheme.Space.s)
                        errand
                    }
                }
            }
            .padding(.top, MonacoTheme.Space.sm)
            .padding(.bottom, MonacoTheme.Space.xs)
            AssetOpenVotesList(proposals: proposals, symbol: symbol, openProposal: openProposal)
        }
        // No identifier on this stack either: it would rename every vote row inside it.
    }
}

/// The line under the member's slice: what it is a slice of, and who holds that.
///
/// "Across your cabals" is the many-cabal sentence. Under a single cabal it read as a
/// line written for someone else, so one holder is named instead.
enum AssetPositionTotalsCopy {
    static func sliceLine(totalValueUsd: String, holdings: [AssetHoldingDTO]) -> String {
        let total = UsdAmountFormatter.format(decimalString: totalValueUsd)
        if holdings.count == 1, let name = holdings.first?.name.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty {
            return "Your slice of \(total) held by \(name)"
        }
        return "Your slice of \(total) held across your cabals"
    }
}

/// The position slot when the social read did not come back.
///
/// One card, not two: the holdings, the votes and the activity are all one call, so
/// one notice explains all three and one retry brings all three back. What it must
/// never do is stay quiet — an absent card reads as "no cabal of yours holds this",
/// which is a claim about the member's money that we have no answer to make.
///
/// The wording is `AssetSocialFailureCopy` (MonacoCore, tested).
struct AssetSocialFailedCard: View {
    let symbol: String
    let retry: () -> Void

    var body: some View {
        AssetDetailCard(
            title: AssetSocialFailureCopy.cardTitle,
            identifier: "asset-detail-position-failed"
        ) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                Label(AssetSocialFailureCopy.message(symbol: symbol), systemImage: "exclamationmark.triangle")
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(AssetSocialFailureCopy.spoken(symbol: symbol))
                Button(AssetSocialFailureCopy.retryTitle, action: retry)
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("asset-position-retry")
            }
        }
    }
}

/// One cabal's line: who, how much, and what it has done.
private struct HoldingRow: View {
    let holding: AssetHoldingDTO
    var openCabal: ((String) -> Void)?

    /// Smaller than a list row's 44pt: this row sits inside a section, under a figure, and
    /// the section's title already says whose cabals these are.
    static let markSize: CGFloat = 40

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if let openCabal {
                Button { openCabal(holding.groupId) } label: { content }
                    .buttonStyle(.plain)
            } else {
                content
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(openCabal == nil ? [] : .isButton)
        .accessibilityIdentifier("asset-position-holding-\(holding.groupId)")
    }

    private var content: some View {
        // Side by side until the text sizes stop allowing it. At the accessibility
        // sizes the name and the money were fighting over one row and both lost:
        // a cabal called "Weekend investors" truncated and the value scaled itself
        // down to a size the member turned the text up to avoid.
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    identity
                    money(alignment: .leading)
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    identity
                    Spacer(minLength: MonacoTheme.Space.s)
                    money(alignment: .trailing)
                    if openCabal != nil { RowChevron() }
                }
            }
        }
        .padding(.vertical, MonacoTheme.Space.s + 2)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
    }

    private var identity: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            CabalMark(groupId: holding.groupId, name: holding.name, size: Self.markSize)
            VStack(alignment: .leading, spacing: 2) {
                Text(holding.name)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(isStacked ? 2 : 1)
                    .fixedSize(horizontal: false, vertical: isStacked)
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(isStacked ? 2 : 1)
                    .minimumScaleFactor(0.85)
                    .fixedSize(horizontal: false, vertical: isStacked)
            }
        }
    }

    private func money(alignment: HorizontalAlignment) -> some View {
        VStack(alignment: alignment, spacing: 2) {
            Text(UsdAmountFormatter.format(decimalString: holding.valueUsd))
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.ink)
            PnLText(dollarPnl: holding.dollarPnl, style: .caption)
        }
    }

    private var shares: String? {
        AssetHoldingShareLabel.text(units: holding.units, tokenAmount: holding.tokenAmount)
    }

    /// "12 shares · $556.92 yours". The shares are the cabal's, the dollars are the
    /// member's own part of them.
    private var subtitle: String {
        let slice = "\(UsdAmountFormatter.format(decimalString: holding.mySliceUsd)) yours"
        guard let shares else { return slice }
        return "\(shares) · \(slice)"
    }

    private var spoken: String {
        var sentence = holding.name
        if let shares { sentence += ", \(shares)" }
        sentence += ", worth \(UsdAmountFormatter.format(decimalString: holding.valueUsd))"
        sentence += ", of which \(UsdAmountFormatter.format(decimalString: holding.mySliceUsd)) is yours. "
        sentence += PnLSpeech.badge(dollarPnl: holding.dollarPnl, percentReturn: holding.percentReturn)
        return sentence
    }
}

/// How many shares a cabal holds, in words: "12 shares", "3.5 shares", "1 share".
///
/// Through the one formatter the cabal screen's holdings use
/// (`ProposalShareFormatter.sharesLabel`), so the same position reads the same on both
/// screens — the token amount when the backend sent one, the whole-token figure
/// otherwise. The row used to print `units` raw with "units" after it, which is a
/// word the product does not say and which made one share "1 units".
///
/// Nil when neither figure is a positive number: the row then says only what is the
/// member's, rather than "0 shares" beside a value.
enum AssetHoldingShareLabel {
    private static let posix = Locale(identifier: "en_US_POSIX")

    static func text(units: String, tokenAmount: String) -> String? {
        let atomics = tokenAmount.trimmingCharacters(in: .whitespaces)
        if let value = Decimal(string: atomics, locale: posix), value > 0 {
            return ProposalShareFormatter.sharesLabel(fromAtomics: atomics)
        }
        guard let whole = Decimal(string: units.trimmingCharacters(in: .whitespaces), locale: posix), whole > 0 else {
            return nil
        }
        let scaled = whole * Decimal(sign: .plus, exponent: ProposalShareFormatter.defaultDecimals, significand: 1)
        return ProposalShareFormatter.sharesLabel(fromAtomics: NSDecimalNumber(decimal: scaled).stringValue)
    }
}

/// The open votes on this stock, each with the faces behind it, as ruled rows.
private struct AssetOpenVotesList: View {
    let proposals: [AssetProposalDTO]
    let symbol: String
    var openProposal: ((AssetProposalDTO) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(proposals.enumerated()), id: \.element.id) { index, proposal in
                if index > 0 { AssetCardDivider() }
                VoteRow(proposal: proposal, openProposal: openProposal)
            }
        }
    }
}

private struct VoteRow: View {
    let proposal: AssetProposalDTO
    var openProposal: ((AssetProposalDTO) -> Void)?

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var isStacked: Bool { dynamicTypeSize.isAccessibilitySize }

    var body: some View {
        Group {
            if let openProposal {
                Button { openProposal(proposal) } label: { content }
                    .buttonStyle(.plain)
            } else {
                content
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(openProposal == nil ? [] : .isButton)
        .accessibilityIdentifier("asset-position-vote-\(proposal.id)")
    }

    private var content: some View {
        Group {
            if isStacked {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    details
                    status
                }
            } else {
                HStack(spacing: MonacoTheme.Space.sm) {
                    details
                    Spacer(minLength: MonacoTheme.Space.s)
                    status
                }
            }
        }
        .padding(.vertical, MonacoTheme.Space.s + 2)
        .frame(minHeight: 60)
        .contentShape(Rectangle())
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(MonacoTheme.Typo.rowTitle)
                .foregroundStyle(MonacoTheme.ink)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: MonacoTheme.Space.s) {
                if !proposal.yesVoters.isEmpty {
                    VoterFaces(voters: proposal.yesVoters)
                }
                Text(tally)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
        }
    }

    /// Where the row stands, and where it goes.
    ///
    /// The "Vote" capsule used to be brand-filled at 44pt: it looked like the primary
    /// action on the card and did nothing at all — no ballot is cast on this screen,
    /// and nothing was wired behind it. It is a badge now, in the same wash as the
    /// "waiting on your vote" pill above it, saying what this vote's state is. The
    /// chevron says where the row leads, and the row leads to the proposal screen,
    /// which is where a ballot has always been cast.
    ///
    /// A cast ballot is the word "Voted" in the quiet colour, not a green tick: green on
    /// this screen means a price went up, and a vote is not a gain.
    private var status: some View {
        HStack(spacing: MonacoTheme.Space.xs) {
            if proposal.myVote == nil {
                Text("Vote")
                    .font(MonacoTheme.Typo.captionStrong)
                    .foregroundStyle(MonacoTheme.brandOnWash)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Capsule().fill(MonacoTheme.brandWash))
            } else {
                Text("Voted")
                    .font(MonacoTheme.Typo.captionStrong)
                    .foregroundStyle(MonacoTheme.muted)
            }
            if openProposal != nil {
                RowChevron()
            }
        }
    }

    /// "Weekend investors · buy $500.00", "Desk lunch money · sell 1.5 shares" — the
    /// cabal leads, because the member is reading about their friends.
    private var title: String {
        let cabal = proposal.groupName.isEmpty ? "A cabal" : proposal.groupName
        switch proposal.kind {
        case .buy where proposal.usdcMicros > 0:
            return "\(cabal) · buy \(UsdAmountFormatter.format(micros: proposal.usdcMicros))"
        case .sell where proposal.tokenAmount > 0:
            // "sell 1.5 shares", not "sell 1.5": a bare number beside "buy $500.00"
            // reads as dollars.
            return "\(cabal) · sell \(ProposalShareFormatter.sharesLabel(fromAtomics: String(proposal.tokenAmount)))"
        case .buy:
            return "\(cabal) · buy"
        case .sell:
            return "\(cabal) · sell"
        case .unknown:
            return cabal
        }
    }

    private var tally: String {
        guard proposal.memberCount > 0 else { return "\(proposal.yes) yes · \(proposal.no) no" }
        return "\(proposal.yes) of \(proposal.memberCount) yes"
    }

    private var spoken: String {
        var sentence = "\(title). \(tally)."
        // The faces are decoration and this row is one element, so without this the
        // one thing #341 is about — which of your friends is already behind this —
        // never reached VoiceOver at all.
        if let voters = AssetPositionSummary.yesVoterSentence(proposal.yesVoters) {
            sentence += " \(voters)."
        }
        if proposal.myVote == nil {
            sentence += " Waiting on your vote."
        } else {
            sentence += " You voted."
        }
        if openProposal != nil {
            sentence += " Opens the vote."
        }
        return sentence
    }
}

/// The affordance that says a row goes somewhere, in the shape `MonacoRow` uses.
/// Only drawn on rows that actually navigate — a chevron on a row that does nothing
/// is the same lie as a filled capsule that does nothing.
private struct RowChevron: View {
    var body: some View {
        Image(systemName: "chevron.right")
            .font(MonacoTheme.Typo.captionStrong)
            .foregroundStyle(MonacoTheme.tertiaryText)
            .accessibilityHidden(true)
    }
}

/// The faces of the members who voted yes, overlapped the way a group avatar row is.
private struct VoterFaces: View {
    let voters: [AssetVoterDTO]

    private var shown: [AssetVoterDTO] { Array(voters.prefix(4)) }

    var body: some View {
        HStack(spacing: -9) {
            ForEach(shown) { voter in
                MonacoAvatar(photoURL: voter.profilePhotoUrl, displayName: voter.displayName, size: 22, seed: voter.userId)
                    // A ring of the paper behind each face, so the overlap reads as a cut
                    // and every face keeps its own hairline. It used to be a white stroke
                    // drawn over the face, from when this sat on a white card; on the
                    // canvas that drew a white outline around each one.
                    .padding(2)
                    .background(Circle().fill(MonacoTheme.canvas))
            }
            if voters.count > shown.count {
                Text("+\(voters.count - shown.count)")
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.muted)
                    .padding(.leading, 12)
            }
        }
        // Decoration: the row is one accessibility element and speaks the voters'
        // names itself, in `VoteRow.spoken`. Unhiding these would read four avatars
        // and a "+2" between the vote's title and its tally.
        .accessibilityHidden(true)
    }
}
