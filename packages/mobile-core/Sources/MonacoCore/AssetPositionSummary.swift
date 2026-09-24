import Foundation

/// "Your cabals' position": the arithmetic and the sentences behind the card that
/// makes this screen Monaco's and not a broker's.
///
/// The totals are summed in `Decimal` from the backend's decimal strings rather than
/// in `Double`: a pot of a few hundred thousand dollars already loses cents to binary
/// floating point, and this figure sits next to a member's own slice.
public struct AssetPositionSummary: Equatable, Sendable {
    /// Every cabal that holds the symbol, biggest position first.
    public let holdings: [AssetHoldingDTO]
    /// What all of them hold together, as a decimal string.
    public let totalValueUsd: String
    /// The viewer's own share of that, as a decimal string.
    public let myTotalSliceUsd: String
    /// The cabals' combined P&L on this symbol, signed.
    ///
    /// The cabals'. Not the viewer's: it is `sum(valueUsd) - sum(costBasisUsd)` over
    /// every cabal, and a member holding a fifth of those pots earned nothing like
    /// all of it. Whatever draws this must say whose number it is — see
    /// `totalPnlLabel`.
    public let totalDollarPnl: String
    /// The same as a ratio ("0.0710"), or nil when there is no cost basis behind the
    /// position to measure a return against. Also the cabals'.
    public let totalPercentReturn: String?
    /// Whose the P&L figures are, in words: "Your cabals' return".
    ///
    /// The card used to set `myTotalSliceUsd` in the big money font with the badge on
    /// the same baseline, which reads as one pair — a slice of $837.32 that is up
    /// $323.83. It is not: the $323.83 is what the cabals made, and the member's share
    /// of it is about a fifth. We cannot fix that by scaling, because nothing on the
    /// wire supports it: `/v1/assets/{symbol}/social` sends the viewer's share of each
    /// position's *value* (`mySliceUsd`, share units over share base) and no basis
    /// behind it, and a pot's share units are bought in at the NAV of the day someone
    /// joined — so a member who joined last week did not earn last year's gain, and
    /// `pnl x mySlicePercent` would hand it to them. The honest per-member figure the
    /// backend does compute (`MemberSliceDTO.dollarPnl`) is per cabal, not per symbol.
    /// So the pair is labelled instead of being invented.
    ///
    /// Nil when no cabal holds the symbol, so a card that is only carrying an
    /// "unvalued" notice does not print a $0.00 return under it.
    public let totalPnlLabel: String?
    /// "3 cabals hold AAPLx" / "One cabal holds AAPLx".
    public let headline: String
    /// "2 open votes on AAPLx", or nil when there are none.
    public let voteHeadline: String?
    /// Set when one of those votes is waiting on the viewer. This is the line that
    /// turns a card into an errand.
    public let waitingOnYou: String?
    /// "1 cabal could not be priced just now" — never silence. Telling a member no
    /// cabal holds a stock on a partial answer would be a lie about their money.
    public let unvaluedNotice: String?

    public var isEmpty: Bool { holdings.isEmpty }

    public static func make(_ social: AssetSocialDTO?, symbol: String) -> AssetPositionSummary? {
        guard let social, !social.isEmpty else { return nil }

        let ticker = AssetSymbolFormatter.format(symbol)
        let holdings = social.holdings
        let value = sum(holdings.map(\.valueUsd))
        let slice = sum(holdings.map(\.mySliceUsd))
        let basis = sum(holdings.map(\.costBasisUsd))
        let pnl = value - basis

        return AssetPositionSummary(
            holdings: holdings,
            totalValueUsd: decimalString(value),
            myTotalSliceUsd: decimalString(slice),
            totalDollarPnl: signedDecimalString(pnl),
            totalPercentReturn: basis > 0 ? ratioString(pnl / basis) : nil,
            totalPnlLabel: pnlLabel(holdingCount: holdings.count),
            headline: headline(holdingCount: holdings.count, ticker: ticker),
            voteHeadline: voteHeadline(social.openProposals, ticker: ticker),
            waitingOnYou: waitingOnYou(social.openProposals),
            unvaluedNotice: unvaluedNotice(social.unvaluedGroups)
        )
    }

    /// Whose return the badge measures. The possessive is the whole job of this
    /// string: without it the figure above and the badge beside it read as one pair,
    /// and they are about two different sets of money.
    static func pnlLabel(holdingCount: Int) -> String? {
        switch holdingCount {
        case 0: return nil
        case 1: return "Your cabal's return"
        default: return "Your cabals' return"
        }
    }

    static func headline(holdingCount: Int, ticker: String) -> String {
        switch holdingCount {
        case 0: return "No cabal of yours holds \(ticker) yet"
        case 1: return "One cabal holds \(ticker)"
        default: return "\(holdingCount) cabals hold \(ticker)"
        }
    }

    static func voteHeadline(_ proposals: [AssetProposalDTO], ticker: String) -> String? {
        switch proposals.count {
        case 0: return nil
        case 1: return "1 open vote on \(ticker)"
        default: return "\(proposals.count) open votes on \(ticker)"
        }
    }

    /// Only the votes the viewer has not cast a ballot on.
    static func waitingOnYou(_ proposals: [AssetProposalDTO]) -> String? {
        let pending = proposals.filter { $0.myVote == nil }
        switch pending.count {
        case 0: return nil
        case 1: return "1 is waiting on your vote"
        default: return "\(pending.count) are waiting on your vote"
        }
    }

    /// "Ada and Bo voted yes", or nil when nobody has.
    ///
    /// The card draws these as overlapped faces, which VoiceOver cannot read — the
    /// avatars are decoration and the row is one accessibility element. Who voted is
    /// half of what makes an open vote worth looking at (#341), so it is said in
    /// words. Three names, then a count: a sentence naming nine people is not a
    /// sentence anyone listens to.
    public static func yesVoterSentence(_ voters: [AssetVoterDTO]) -> String? {
        let names = voters
            .map { $0.displayName.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        guard !names.isEmpty else { return nil }
        let shown = Array(names.prefix(3))
        let rest = names.count - shown.count
        var tail = shown
        if rest > 0 {
            tail.append(rest == 1 ? "1 other" : "\(rest) others")
        }
        let list: String
        switch tail.count {
        case 1: list = tail[0]
        case 2: list = "\(tail[0]) and \(tail[1])"
        default: list = tail.dropLast().joined(separator: ", ") + " and " + tail[tail.count - 1]
        }
        return "\(list) voted yes"
    }

    static func unvaluedNotice(_ count: Int) -> String? {
        switch count {
        case 0: return nil
        case 1: return "1 cabal could not be priced just now"
        default: return "\(count) cabals could not be priced just now"
        }
    }

    // MARK: - Decimal arithmetic

    private static let posix = Locale(identifier: "en_US_POSIX")

    static func sum(_ raw: [String]) -> Decimal {
        raw.reduce(Decimal(0)) { total, value in
            total + (Decimal(string: value.trimmingCharacters(in: .whitespaces), locale: posix) ?? 0)
        }
    }

    static func decimalString(_ value: Decimal) -> String {
        var rounded = Decimal()
        var source = value
        NSDecimalRound(&rounded, &source, 2, .plain)
        return (rounded as NSDecimalNumber).stringValue
    }

    /// The shape `PnLText` and `MonacoTheme.signed` already read: "+184.60" / "-50.00".
    static func signedDecimalString(_ value: Decimal) -> String {
        let body = decimalString(value)
        return value > 0 ? "+\(body)" : body
    }

    /// A backend-style ratio, always fixed-point: `String(someDouble)` switches to
    /// scientific notation below 1e-4 and "5e-05" is read digit by digit downstream
    /// as a gain of 505%.
    static func ratioString(_ value: Decimal) -> String {
        var rounded = Decimal()
        var source = value
        NSDecimalRound(&rounded, &source, 6, .plain)
        return (rounded as NSDecimalNumber).stringValue
    }
}
