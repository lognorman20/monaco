import MonacoCore
import Testing
@testable import Monaco

// The app target shadows these MonacoCore DTOs; pin the tests to the ones the views use.
private typealias AssetSocialDTO = Monaco.AssetSocialDTO
private typealias AssetHoldingDTO = Monaco.AssetHoldingDTO
private typealias AssetProposalDTO = Monaco.AssetProposalDTO

@MainActor
private final class StubAssetSocialDataSource: AssetSocialDataSource {
    var calls = 0
    var answer: AssetSocialDTO = AssetSocialSampleData.social()
    var error: Error?

    func social(symbol: String) async throws -> AssetSocialDTO {
        calls += 1
        if let error { throw error }
        return answer
    }
}

private struct SocialFailure: Error {}

@MainActor
private func model(_ source: StubAssetSocialDataSource, symbol: String = "AAPLx") -> AssetSocialModel {
    AssetSocialModel(symbol: symbol, dataSource: source)
}

@Suite("Asset social model")
struct AssetSocialModelTests {
    @Test("A loaded answer builds the position card")
    @MainActor
    func loadsSummary() async {
        let source = StubAssetSocialDataSource()
        let model = model(source)

        await model.load()

        #expect(model.state == .answered)
        #expect(!model.hasFailed)
        #expect(model.summary?.headline == "3 cabals hold AAPLx")
        #expect(model.openProposals.count == 2)
        #expect(model.activity.count == 4)
    }

    /// The cards are additions to a screen that already works, so a social read that
    /// fails leaves the price, the chart and the buy button exactly as they were —
    /// but it is a failure, not an answer of "no cabal of yours holds this". That
    /// distinction is what the position card and the trade bar hang off.
    @Test("A failed read is a failure state, not an empty answer")
    @MainActor
    func failureIsAFailure() async {
        let source = StubAssetSocialDataSource()
        source.error = SocialFailure()
        let model = model(source)

        await model.load()

        #expect(model.state == .failed)
        #expect(model.hasFailed)
        #expect(model.summary == nil)
        #expect(model.holdings.isEmpty)
        #expect(!model.sessionExpired)
    }

    /// The whole point of the failure state: with nothing in hand the bar must not
    /// let a missing Sell button tell a member they hold none of this.
    @Test("A failed read stops the trade bar claiming nothing is held")
    @MainActor
    func failureReachesTheTradeBar() async {
        let source = StubAssetSocialDataSource()
        source.error = SocialFailure()
        let model = model(source)

        await model.load()

        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: model.holdings,
            holdingsState: model.state
        )
        #expect(bar.sell == .unknown(notice: AssetSocialFailureCopy.sellUnknown))
        #expect(bar.canBuy)
    }

    /// A retry after a failure is the way back, and it brings every card with it.
    @Test("A retry after a failure rebuilds the cards")
    @MainActor
    func retryAfterFailure() async {
        let source = StubAssetSocialDataSource()
        source.error = SocialFailure()
        let model = model(source)
        await model.load()
        #expect(model.hasFailed)

        source.error = nil
        await model.load()

        #expect(model.state == .answered)
        #expect(!model.hasFailed)
        #expect(model.summary?.headline == "3 cabals hold AAPLx")
        #expect(model.activity.count == 4)
    }

    /// A re-read that fails must not empty a card the member is looking at, and must
    /// not turn a screen full of holdings into a failure notice either.
    @Test("A failed refresh keeps the cards already on screen")
    @MainActor
    func failedRefreshKeepsCards() async {
        let source = StubAssetSocialDataSource()
        let model = model(source)
        await model.load()
        #expect(model.holdings.count == 3)

        source.error = SocialFailure()
        await model.refresh()

        #expect(model.holdings.count == 3)
        #expect(model.summary?.headline == "3 cabals hold AAPLx")
        #expect(!model.hasFailed)
    }

    @Test("A rejected session is reported, not swallowed")
    @MainActor
    func sessionExpiry() async {
        let source = StubAssetSocialDataSource()
        source.error = MonacoAPIError.httpStatus(401)
        let model = model(source)

        await model.load()

        #expect(model.sessionExpired)
    }

    @Test("Nothing to show builds no card at all")
    @MainActor
    func emptyBuildsNothing() async {
        let source = StubAssetSocialDataSource()
        source.answer = AssetSocialSampleData.empty()
        let model = model(source)

        await model.load()

        #expect(model.summary == nil)
        #expect(model.activity.isEmpty)
    }

    /// A cabal that could not be priced is still something to say, so the card draws
    /// even though nothing is held and nothing is being voted on.
    @Test("A cabal that could not be priced keeps the card on screen")
    @MainActor
    func unvaluedKeepsCard() async {
        let source = StubAssetSocialDataSource()
        source.answer = AssetSocialDTO(symbol: "AAPLx", unvaluedGroups: 2)
        let model = model(source)

        await model.load()

        #expect(model.summary?.unvaluedNotice == "2 cabals could not be priced just now")
    }
}

@Suite("Asset trade bar")
struct AssetTradeBarWiringTests {
    /// The dead end this bar exists to remove: Sell used to be offered
    /// unconditionally and then walked the member to "This cabal does not hold this
    /// stock."
    @Test("Sell is hidden until a cabal actually holds the stock")
    @MainActor
    func sellFollowsHoldings() async {
        let source = StubAssetSocialDataSource()
        source.answer = AssetSocialSampleData.empty()
        let model = model(source)
        await model.load()

        let empty = AssetTradeBarState.make(
            isRoutable: true,
            holdings: model.holdings,
            holdingsState: model.state
        )
        #expect(!empty.showsSell)
        #expect(empty.sell == .hidden, "an answer of nothing is hidden, not unknown")

        source.answer = AssetSocialSampleData.modest()
        await model.refresh()
        let held = AssetTradeBarState.make(
            isRoutable: true,
            holdings: model.holdings,
            holdingsState: model.state
        )
        #expect(held.showsSell)
        #expect(held.sell == .available(caption: "Weekend investors"))
    }

    /// Before the answer lands, "no cabal holds this" is not yet a fact, so the bar
    /// says nothing about selling rather than saying the wrong thing.
    @Test("Before the answer lands the bar holds its sell back")
    @MainActor
    func sellWaitsForTheAnswer() {
        let bar = AssetTradeBarState.make(
            isRoutable: true,
            holdings: AssetSocialSampleData.modest().holdings,
            holdingsState: .loading
        )
        #expect(!bar.showsSell)
    }

    @Test("An unroutable token explains itself next to the button it disables")
    @MainActor
    func unroutableExplainsItself() {
        let bar = AssetTradeBarState.make(isRoutable: false, holdings: [], holdingsState: .answered)
        #expect(!bar.canBuy)
        #expect(bar.buyDisabledReason != nil)
    }
}
