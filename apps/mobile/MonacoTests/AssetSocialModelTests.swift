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

/// A source whose answers the test hands out by hand, in whatever order it likes.
/// That is the only way to put an older response behind a newer one on purpose.
@MainActor
private final class GatedAssetSocialDataSource: AssetSocialDataSource {
    private(set) var calls = 0
    private var pending: [CheckedContinuation<Result<AssetSocialDTO, Error>, Never>] = []

    func social(symbol: String) async throws -> AssetSocialDTO {
        calls += 1
        let result = await withCheckedContinuation { pending.append($0) }
        return try result.get()
    }

    /// Answers the `index`th call (0-based, in issue order).
    func answer(_ index: Int, with result: Result<AssetSocialDTO, Error>) {
        pending[index].resume(returning: result)
    }

    /// Lets queued main-actor tasks run until `count` reads are in flight.
    func waitForCalls(_ count: Int) async {
        for _ in 0..<1_000 where calls < count {
            await Task.yield()
        }
    }
}

@MainActor
private func model(_ source: StubAssetSocialDataSource, symbol: String = "AAPLc") -> AssetSocialModel {
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
        #expect(model.summary?.headline == "3 cabals hold AAPLc")
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
        #expect(model.rejectedSession == nil)
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
        #expect(model.summary?.headline == "3 cabals hold AAPLc")
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
        #expect(model.summary?.headline == "3 cabals hold AAPLc")
        #expect(!model.hasFailed)
    }

    /// The race: the screen's first read is still out when a proposal lands and the
    /// propose callback re-reads. The newer answer — the one that counts the new
    /// vote — comes back first, and the older one must not land on top of it.
    @Test("An older answer that lands last does not overwrite a newer one")
    @MainActor
    func staleAnswerIsDropped() async {
        let source = GatedAssetSocialDataSource()
        let model = AssetSocialModel(symbol: "AAPLc", dataSource: source)

        let onAppear = Task { await model.load() }
        await source.waitForCalls(1)
        let afterProposing = Task { await model.refresh() }
        await source.waitForCalls(2)
        #expect(source.calls == 2)

        source.answer(1, with: .success(AssetSocialSampleData.modest()))
        await afterProposing.value
        #expect(model.summary?.headline == "One cabal holds AAPLc")

        source.answer(0, with: .success(AssetSocialSampleData.social()))
        await onAppear.value

        #expect(model.summary?.headline == "One cabal holds AAPLc", "the pre-proposal answer repainted the card")
        #expect(model.openProposals.count == 1)
        #expect(model.state == .answered)
    }

    /// An overtaken read that fails says nothing about the screen: the newer read
    /// has already answered for it.
    @Test("An older failure that lands last leaves the newer answer alone")
    @MainActor
    func staleFailureIsDropped() async {
        let source = GatedAssetSocialDataSource()
        let model = AssetSocialModel(symbol: "AAPLc", dataSource: source)

        let first = Task { await model.load() }
        await source.waitForCalls(1)
        let second = Task { await model.refresh() }
        await source.waitForCalls(2)

        source.answer(1, with: .success(AssetSocialSampleData.social()))
        await second.value
        source.answer(0, with: .failure(RejectedSession(token: "stale-token")))
        await first.value

        #expect(model.state == .answered)
        #expect(model.holdings.count == 3)
        #expect(model.rejectedSession == nil, "an overtaken read does not get to end the session either")
    }

    /// And the ordinary case still works: answers that come back in issue order
    /// leave the newest on screen.
    @Test("Answers in issue order leave the newest on screen")
    @MainActor
    func inOrderAnswers() async {
        let source = GatedAssetSocialDataSource()
        let model = AssetSocialModel(symbol: "AAPLc", dataSource: source)

        let first = Task { await model.load() }
        await source.waitForCalls(1)
        let second = Task { await model.refresh() }
        await source.waitForCalls(2)

        source.answer(0, with: .success(AssetSocialSampleData.social()))
        await first.value
        source.answer(1, with: .success(AssetSocialSampleData.modest()))
        await second.value

        #expect(model.summary?.headline == "One cabal holds AAPLc")
    }

    @Test("A rejected session is reported, not swallowed")
    @MainActor
    func sessionExpiry() async {
        let source = StubAssetSocialDataSource()
        // What the guarded auth API throws: the rejection carries the token that
        // request sent, so the view ends that session and not whichever one has
        // replaced it since.
        source.error = RejectedSession(token: "token-that-was-refused")
        let model = model(source)

        await model.load()

        #expect(model.rejectedSession?.token == "token-that-was-refused")
        #expect(model.state == .loading, "a rejected session is not an answer about the holdings")
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
        source.answer = AssetSocialDTO(symbol: "AAPLc", unvaluedGroups: 2)
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
