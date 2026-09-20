import Foundation

/// What a "Your cabals" row says on its trailing edge, on Home and on Profile.
///
/// The big figure is always what the member *has* in the cabal. It used to be the dollar P&L,
/// and a flat P&L rendered as "$0.00" — which a member with a $2.00 slice read as "I have
/// nothing here". The change since they joined sits underneath, smaller, as a signed figure.
public struct CabalPositionRowFigures: Equatable, Sendable {
    /// The line under the member's equity.
    public enum Change: Equatable, Sendable {
        /// A backend return ratio, for `PercentReturnFormatter` ("0.096" → "+9.6%").
        case percent(String)
        /// A signed dollar amount, for `SignedUsdFormatter` ("+27.40" → "+$27.40").
        case dollars(String)
        /// The server sent nothing usable; the row shows "—" rather than claiming "no change".
        case unavailable
    }

    /// The member's slice of the cabal, as the server's decimal string.
    public let equityUsd: String
    public let change: Change

    /// Percent when the server has one. Without it, a P&L that moved is shown in dollars, and
    /// one that did not is "0.0%" — never a second dollar figure that reads as a balance.
    public init(equityUsd: String, dollarPnl: String, percentReturn: String?) {
        self.equityUsd = equityUsd
        if let percentReturn, PercentReturnFormatter.format(percentReturn) != "—" {
            change = .percent(percentReturn)
        } else if SignedUsdFormatter.parse(dollarPnl) == nil {
            change = .unavailable
        } else if SignedUsdFormatter.isZero(dollarPnl) {
            change = .percent("0")
        } else {
            change = .dollars(dollarPnl)
        }
    }

    /// "Pot $1,900.00", the row's subtitle. Nil until the pot value has loaded.
    public static func potSubtitle(potValueUsd: String?) -> String? {
        guard let potValueUsd else { return nil }
        return "Pot \(UsdAmountFormatter.format(decimalString: potValueUsd))"
    }
}
