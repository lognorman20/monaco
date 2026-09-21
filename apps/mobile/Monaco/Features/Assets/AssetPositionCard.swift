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

    var body: some View {
        AssetDetailCard(
            title: "Your cabals' position",
            identifier: "asset-detail-position"
        ) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                totals
                if !summary.holdings.isEmpty {
                    AssetCardDivider()
                    ForEach(Array(summary.holdings.enumerated()), id: \.element.id) { index, holding in
                        HoldingRow(holding: holding, openCabal: openCabal)
                        if index < summary.holdings.count - 1 {
                            AssetCardDivider()
                        }
                    }
                }
                if let notice = summary.unvaluedNotice {
                    // Never silence. A cabal we could not price is not a cabal that
                    // holds nothing, and the difference is someone's money.
                    Label(notice, systemImage: "exclamationmark.triangle")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.warning)
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

            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                // The member's own slice is the headline figure. The cabals' total
                // is context underneath it — they came to see their own money.
                Text(UsdAmountFormatter.format(decimalString: summary.myTotalSliceUsd))
                    .moneyFont(.large)
                    .foregroundStyle(MonacoTheme.ink)
                Spacer(minLength: MonacoTheme.Space.s)
                PnLBadge(dollarPnl: summary.totalDollarPnl, percentReturn: summary.totalPercentReturn)
            }

            Text("Your slice of \(UsdAmountFormatter.format(decimalString: summary.totalValueUsd)) held across your cabals")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spokenTotals)
        .accessibilityIdentifier("asset-position-totals")
    }

    private var spokenTotals: String {
        var sentence = "\(summary.headline). Your slice, "
        sentence += UsdAmountFormatter.format(decimalString: summary.myTotalSliceUsd)
        sentence += ", of \(UsdAmountFormatter.format(decimalString: summary.totalValueUsd)) held. "
        sentence += PnLSpeech.badge(dollarPnl: summary.totalDollarPnl, percentReturn: summary.totalPercentReturn)
        return sentence
    }

    // MARK: - Votes

    private func votes(_ headline: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                Text(headline)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                Spacer(minLength: MonacoTheme.Space.s)
                if let waiting = summary.waitingOnYou {
                    Text(waiting)
                        .font(MonacoTheme.Typo.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.brandOnWash)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(Capsule().fill(MonacoTheme.brandWash))
                        .accessibilityIdentifier("asset-position-waiting-on-you")
                }
            }
            AssetOpenVotesList(proposals: proposals, symbol: symbol, openProposal: openProposal)
        }
        // No identifier on this stack either: it would rename every vote row inside it.
    }
}

/// One cabal's line: who, how much, and what it has done.
private struct HoldingRow: View {
    let holding: AssetHoldingDTO
    var openCabal: ((String) -> Void)?

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
        HStack(spacing: MonacoTheme.Space.sm) {
            CabalMark(groupId: holding.groupId, name: holding.name, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(holding.name)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
            }
            Spacer(minLength: MonacoTheme.Space.s)
            VStack(alignment: .trailing, spacing: 2) {
                Text(UsdAmountFormatter.format(decimalString: holding.valueUsd))
                    .moneyFont(.row)
                    .foregroundStyle(MonacoTheme.ink)
                PnLText(dollarPnl: holding.dollarPnl, style: .caption)
            }
        }
        .frame(minHeight: 44)
        .contentShape(Rectangle())
    }

    /// "12 units · $556.92 yours". The units are the cabal's, the dollars are the
    /// member's own share of them.
    private var subtitle: String {
        let slice = UsdAmountFormatter.format(decimalString: holding.mySliceUsd)
        return "\(holding.units) units · \(slice) yours"
    }

    private var spoken: String {
        var sentence = "\(holding.name), \(holding.units) units, worth "
        sentence += UsdAmountFormatter.format(decimalString: holding.valueUsd)
        sentence += ", of which \(UsdAmountFormatter.format(decimalString: holding.mySliceUsd)) is yours. "
        sentence += PnLSpeech.badge(dollarPnl: holding.dollarPnl, percentReturn: holding.percentReturn)
        return sentence
    }
}

/// The open votes on this stock, each with the faces behind it.
private struct AssetOpenVotesList: View {
    let proposals: [AssetProposalDTO]
    let symbol: String
    var openProposal: ((AssetProposalDTO) -> Void)?

    var body: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            ForEach(proposals) { proposal in
                VoteRow(proposal: proposal, openProposal: openProposal)
            }
        }
    }
}

private struct VoteRow: View {
    let proposal: AssetProposalDTO
    var openProposal: ((AssetProposalDTO) -> Void)?

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
        HStack(spacing: MonacoTheme.Space.sm) {
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(MonacoTheme.Typo.rowTitle)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: MonacoTheme.Space.s) {
                    VoterFaces(voters: proposal.yesVoters)
                    Text(tally)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
            }
            Spacer(minLength: MonacoTheme.Space.s)
            if proposal.myVote == nil {
                Text("Vote")
                    .font(MonacoTheme.Typo.caption.weight(.semibold))
                    .foregroundStyle(MonacoTheme.onBrand)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 7)
                    .background(Capsule().fill(MonacoTheme.brandFill))
            } else {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(MonacoTheme.profit)
            }
        }
        .frame(minHeight: 44)
        .contentShape(Rectangle())
    }

    /// "Weekend investors · buy $500.00" — the cabal leads, because the member is
    /// reading about their friends.
    private var title: String {
        let cabal = proposal.groupName.isEmpty ? "A cabal" : proposal.groupName
        switch proposal.kind {
        case .buy where proposal.usdcMicros > 0:
            return "\(cabal) · buy \(UsdAmountFormatter.format(micros: proposal.usdcMicros))"
        case .sell where proposal.tokenAmount > 0:
            return "\(cabal) · sell \(ProposalShareFormatter.shares(fromAtomics: String(proposal.tokenAmount)))"
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
        if proposal.myVote == nil {
            sentence += " Waiting on your vote."
        } else {
            sentence += " You voted."
        }
        return sentence
    }
}

/// The faces of the members who voted yes, overlapped the way a group avatar row is.
private struct VoterFaces: View {
    let voters: [AssetVoterDTO]

    private var shown: [AssetVoterDTO] { Array(voters.prefix(4)) }

    var body: some View {
        HStack(spacing: -8) {
            ForEach(shown) { voter in
                MonacoAvatar(photoURL: voter.profilePhotoUrl, displayName: voter.displayName, size: 22)
                    .overlay(Circle().strokeBorder(MonacoTheme.surface, lineWidth: 2))
            }
            if voters.count > shown.count {
                Text("+\(voters.count - shown.count)")
                    .font(MonacoTheme.Typo.micro)
                    .foregroundStyle(MonacoTheme.muted)
                    .padding(.leading, 12)
            }
        }
        .accessibilityHidden(true)
    }
}
