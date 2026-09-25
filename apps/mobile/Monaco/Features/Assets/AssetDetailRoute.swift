import MonacoCore
import SwiftUI

/// Every screen the stock detail screen can push.
///
/// One route value, the way the Cabals tab does it: the screen owns it, rows hand
/// over a route rather than a view, and a poll that lands mid-push cannot swap the
/// screen a member is already reading. It also keeps this screen to a single
/// `navigationDestination`, which is what lets a holding row and the propose flow
/// coexist — two `navigationDestination(item:)` modifiers on one view race for the
/// same push.
enum AssetDetailRoute: Hashable, Identifiable {
    /// Pick a cabal, then the amount. The stock is already in hand.
    case propose(ProposalPickKind)
    /// One of the member's cabals, from a holding row or a line of activity. The
    /// name is what the row knew, so the pushed screen has its title before it loads.
    case cabal(id: String, name: String?)
    /// One open vote, from a vote row. The proposal screen is where a ballot is
    /// actually cast — this screen only ever counted them.
    case proposal(id: String)
    /// The same pre-IPO company from another issuer, from the variants list.
    case variant(symbol: String)

    var id: Self { self }
}
