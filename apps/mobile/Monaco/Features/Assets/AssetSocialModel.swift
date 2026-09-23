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
    let auth: DynamicAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: DynamicAuthService) {
        self.auth = auth
    }

    func social(symbol: String) async throws -> AssetSocialDTO {
        try await auth.sendingAccessToken { try await apiClient.getAssetSocial(accessToken: $0, symbol: symbol) }
    }
}

/// The social half of the stock screen: holdings, open votes and activity.
///
/// Kept apart from `AssetDetailModel` on purpose. The price and the curve are about
/// the market and are polled every ten seconds; this is about the member's own
/// cabals, changes when somebody votes, and reads every one of their pots. Folding it
/// into the detail model would have tied a heavier read to a fast poll.
///
/// A failure here is quiet, not silent. The cards this feeds are additions to a
/// screen that is already useful without them, so a social read that does not come
/// back leaves the price, the chart and the buy button exactly as they were — but it
/// says that it failed, rather than letting an empty card and a missing Sell button
/// assert that no cabal of the member's holds the stock. That assertion is about
/// their money, and we do not make it on a read we never got.
@Observable
@MainActor
final class AssetSocialModel {
    let symbol: String

    private(set) var social: AssetSocialDTO?
    /// How far the read has got. The trade bar and the position card both read this:
    /// until an answer has landed, "no cabal holds this" is not yet a fact, and a
    /// read that failed never makes it one.
    private(set) var state: AssetSocialLoadState = .loading

    /// Set when the server rejects the session, carrying the token that request used.
    /// The view hands it to `signOutAfterRejectedSession(rejectedToken:)`, which
    /// ignores a token the current sign-in never used — so a 401 that outlived its
    /// sign-in cannot end the session that replaced it.
    private(set) var rejectedSession: RejectedSession?

    private let dataSource: AssetSocialDataSource

    init(symbol: String, dataSource: AssetSocialDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    /// The position card's figures and sentences, or nil when there is nothing to show.
    var summary: AssetPositionSummary? {
        AssetPositionSummary.make(social, symbol: symbol)
    }

    /// True when the cards should draw the failed state and offer a retry: the read
    /// did not come back and there is nothing on screen it could have replaced.
    var hasFailed: Bool { state == .failed }

    var openProposals: [AssetProposalDTO] { social?.openProposals ?? [] }

    var activity: [AssetActivityDTO] { social?.activity ?? [] }

    var holdings: [AssetHoldingDTO] { social?.holdings ?? [] }

    /// A new sign-in starts knowing nothing about *this* member's cabals.
    ///
    /// The holdings and the votes are the signed-in member's own, unlike the price
    /// and the curve, so they are dropped rather than left on screen until the new
    /// read lands. Keeping them would show one member another member's position --
    /// briefly, and only if a sign-in could happen under this screen, but the cost
    /// of being sure is a line. The state goes back to `.loading`, which is the one
    /// state that asserts nothing either way.
    func beginSession() {
        rejectedSession = nil
        social = nil
        state = .loading
    }

    /// Issue order of reads, so only the newest one may write. The same marker the
    /// chart keeps per range, for the same reason: two reads overlap here all the
    /// time — the one `.task` fires on appear, and the one the propose callback fires
    /// the moment a proposal lands — and without it the older answer can arrive last
    /// and put the pre-proposal card back over the one that already counts the new
    /// vote.
    private var requestSequence = 0

    func load() async {
        requestSequence += 1
        let sequence = requestSequence
        do {
            let answer = try await dataSource.social(symbol: symbol)
            guard requestSequence == sequence else { return }
            social = answer
            state = .answered
        } catch {
            // A failure that a newer read has overtaken says nothing about the
            // screen: that read will answer for itself, one way or the other.
            guard requestSequence == sequence else { return }
            if error.isRequestCancellation { return }
            if let rejected = error as? RejectedSession {
                rejectedSession = rejected
                return
            }
            // Keep whatever was already on screen: a failed re-read must not empty a
            // card the member is looking at, and an answer that did land is still the
            // best one we have. With nothing behind the cards, though, the screen has
            // to say the read failed — leaving them unbuilt would have the position
            // card, the activity card and the Sell button all agreeing, wordlessly,
            // that no cabal of theirs holds this.
            state = social == nil ? .failed : .answered
        }
    }

    /// Re-read after the member does something that could change the answer — voting,
    /// or proposing. Never shows a spinner: the cards are already on screen.
    func refresh() async {
        await load()
    }
}
