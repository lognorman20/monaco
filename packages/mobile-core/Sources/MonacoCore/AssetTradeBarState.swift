import Foundation

/// What the sticky bar at the bottom of the stock screen offers, and what it says
/// when it cannot offer it.
///
/// Sell is shown only when one of the viewer's own cabals actually holds the symbol.
/// The old screen offered Sell unconditionally and then walked the member through a
/// cabal picker to a dead end that read "This cabal does not hold this stock." —
/// a button that cannot work is worse than no button.
///
/// Until the holdings answer arrives, Sell is hidden rather than disabled: a control
/// that appears a second after the screen settles is less jarring than one that is
/// there, grey, and then becomes live.
public struct AssetTradeBarState: Equatable, Sendable {
    public enum SellState: Equatable, Sendable {
        /// No cabal of the viewer's holds it, or nothing has answered yet.
        case hidden
        /// At least one cabal holds it; the caption names how many.
        case available(caption: String?)
    }

    /// "Propose buy".
    public let buyTitle: String
    public let canBuy: Bool
    /// Why buying is off, shown inside the bar rather than as a toast after the tap.
    public let buyDisabledReason: String?
    public let sell: SellState
    /// The quiet line above the buttons: what a tap will actually do. A buy here is
    /// a proposal to the cabal, not an order, and the bar should not imply otherwise.
    public let caption: String?

    public var showsSell: Bool {
        if case .available = sell { return true }
        return false
    }

    public static func make(
        isRoutable: Bool,
        liquidityLabel: String? = nil,
        holdings: [AssetHoldingDTO],
        hasLoadedHoldings: Bool
    ) -> AssetTradeBarState {
        let holders = holdings.filter { !$0.units.isEmpty && $0.units != "0" }
        return AssetTradeBarState(
            buyTitle: "Propose buy",
            canBuy: isRoutable,
            buyDisabledReason: isRoutable ? nil : "Can't be bought right now. No route on Jupiter for this token.",
            sell: sellState(holders: holders, hasLoaded: hasLoadedHoldings),
            caption: caption(isRoutable: isRoutable, holders: holders, liquidityLabel: liquidityLabel)
        )
    }

    static func sellState(holders: [AssetHoldingDTO], hasLoaded: Bool) -> SellState {
        guard hasLoaded, !holders.isEmpty else { return .hidden }
        guard holders.count > 1 else { return .available(caption: holders[0].name) }
        return .available(caption: "\(holders.count) cabals")
    }

    /// The bar says what the button means. "Propose buy" already implies a vote, and
    /// the caption says who votes.
    static func caption(isRoutable: Bool, holders: [AssetHoldingDTO], liquidityLabel: String?) -> String? {
        guard isRoutable else { return nil }
        if holders.isEmpty {
            return "Your cabal votes before anything is bought"
        }
        return "Your cabal votes before anything is bought or sold"
    }
}
