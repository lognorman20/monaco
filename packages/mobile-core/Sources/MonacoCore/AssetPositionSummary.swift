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
    public let totalDollarPnl: String
    /// The same as a ratio ("0.0710"), or nil when there is no cost basis behind the
    /// position to measure a return against.
    public let totalPercentReturn: String?
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
            headline: headline(holdingCount: holdings.count, ticker: ticker),
            voteHeadline: voteHeadline(social.openProposals, ticker: ticker),
            waitingOnYou: waitingOnYou(social.openProposals),
            unvaluedNotice: unvaluedNotice(social.unvaluedGroups)
        )
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
