import MonacoCore
import Observation
import SwiftUI

/// Reads "what are my cabals doing with this stock". The live source calls the API;
/// tests and the sample harness swap in a stub.
@MainActor
protocol AssetSocialDataSource {
    func social(symbol: String) async throws -> AssetSocialDTO
}

@MainActor
struct LiveAssetSocialDataSource: AssetSocialDataSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func social(symbol: String) async throws -> AssetSocialDTO {
        try await auth.withAccessToken { try await apiClient.getAssetSocial(accessToken: $0, symbol: symbol) }
    }
}

/// The social half of the stock screen: holdings, open votes and activity.
///
/// Kept apart from `AssetDetailModel` on purpose. The price and the curve are about
/// the market and are polled every ten seconds; this is about the member's own
/// cabals, changes when somebody votes, and costs a pot valuation per cabal to read.
/// Folding it into the detail model would have tied a heavy read to a fast poll.
///
/// A failure here is silent. The cards this feeds are additions to a screen that is
/// already useful without them, so a social read that does not come back leaves the
/// price, the chart and the buy button exactly as they were.
@Observable
@MainActor
final class AssetSocialModel {
    let symbol: String

    private(set) var social: AssetSocialDTO?
    /// True once a read has completed, whichever way it went. The trade bar reads
    /// this: until an answer has landed, "no cabal holds this" is not yet a fact.
    private(set) var hasAnswered = false

    /// Set when the server rejects the session. The data source has already ended it.
    private(set) var sessionExpired = false

    private let dataSource: AssetSocialDataSource

    init(symbol: String, dataSource: AssetSocialDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    /// The position card's figures and sentences, or nil when there is nothing to show.
    var summary: AssetPositionSummary? {
        AssetPositionSummary.make(social, symbol: symbol)
    }

    var openProposals: [AssetProposalDTO] { social?.openProposals ?? [] }

    var activity: [AssetActivityDTO] { social?.activity ?? [] }

    var holdings: [AssetHoldingDTO] { social?.holdings ?? [] }

    func load() async {
        do {
            social = try await dataSource.social(symbol: symbol)
            hasAnswered = true
        } catch {
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(401) = error {
                sessionExpired = true
                return
            }
            // Keep whatever was already on screen. A failed re-read must not empty
            // a card the member is looking at, and a first failure simply leaves the
            // cards unbuilt rather than putting an error under the chart.
            hasAnswered = true
        }
    }

    /// Re-read after the member does something that could change the answer — voting,
    /// or proposing. Never shows a spinner: the cards are already on screen.
    func refresh() async {
        await load()
    }
}
