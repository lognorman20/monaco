import Foundation

/// What the Stocks tab, the stock detail and the cabal picker read from.
///
/// The app leaves every member nil, and each screen then builds its live, API-backed source from
/// the auth service. The Debug sample harness fills them in so the whole Stocks flow runs from
/// fixed data with no sign-in, which is what the Stocks UI tests drive.
struct StocksFlowSources {
    var tab: StocksTabDataSource?
    var detail: AssetDetailDataSource?
    var holdings: CabalHoldingsDataSource?
    var propose: ProposeService?

    static let live = StocksFlowSources()
}
