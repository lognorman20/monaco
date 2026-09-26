import Foundation
import MonacoCore
import Testing
@testable import Monaco

private struct NewsFailure: Error {}

/// Answers each read from a script, in order, and counts them.
@MainActor
private final class ScriptedNews {
    var answers: [Result<NewsFeedDTO, Error>]
    private(set) var calls = 0

    init(_ answers: [Result<NewsFeedDTO, Error>]) {
        self.answers = answers
    }

    func read() async throws -> NewsFeedDTO {
        calls += 1
        return try answers.removeFirst().get()
    }
}

/// A source whose answers the test hands out by hand, so an older read can be made to
/// land after a newer one.
@MainActor
private final class GatedNews {
    private(set) var calls = 0
    private var pending: [CheckedContinuation<Result<NewsFeedDTO, Error>, Never>] = []

    func read() async throws -> NewsFeedDTO {
        calls += 1
        let result = await withCheckedContinuation { pending.append($0) }
        return try result.get()
    }

    func answer(_ index: Int, with result: Result<NewsFeedDTO, Error>) {
        pending[index].resume(returning: result)
    }

    func waitForCalls(_ count: Int) async {
        for _ in 0..<1_000 where calls < count {
            await Task.yield()
        }
    }
}

private let now = Date(timeIntervalSince1970: 1_790_378_100) // 2026-09-25T23:15:00Z

@MainActor
private func model(_ source: ScriptedNews) -> NewsFeedModel {
    NewsFeedModel(fetch: { try await source.read() }, now: { now })
}

@Suite("News feed model")
struct NewsFeedModelTests {
    @Test("An answer builds the rows, newest first as the server sent them")
    @MainActor
    func answered() async {
        let source = ScriptedNews([.success(NewsSampleData.apple(now: now))])
        let model = model(source)
        #expect(model.phase == .loading)

        await model.load()

        #expect(model.phase == .answered)
        #expect(model.headlines.count == 8)
        #expect(model.headlines.first?.stamp == "Yahoo Finance · 12m")
        #expect(!model.isEmpty)
    }

    @Test("A feed with nothing in it is empty, not failed")
    @MainActor
    func empty() async {
        let model = model(ScriptedNews([.success(NewsSampleData.empty(now: now))]))

        await model.load()

        #expect(model.phase == .answered)
        #expect(model.isEmpty)
    }

    @Test("A first read that fails is a failure the section can retry")
    @MainActor
    func failure() async {
        let source = ScriptedNews([.failure(NewsFailure()), .success(NewsSampleData.market(now: now))])
        let model = model(source)

        await model.load()
        #expect(model.phase == .failed)

        await model.load()
        #expect(model.phase == .answered)
        #expect(model.headlines.count == 4)
        #expect(source.calls == 2)
    }

    /// The five-minute re-read is nobody's request, so a failure there must not take the
    /// headlines a member is reading off the screen.
    @Test("A re-read that fails keeps the rows on screen")
    @MainActor
    func refreshFailureKeepsRows() async {
        let model = model(ScriptedNews([.success(NewsSampleData.apple(now: now)), .failure(NewsFailure())]))
        await model.load()

        await model.refresh()

        #expect(model.phase == .answered)
        #expect(model.headlines.count == 8)
    }

    @Test("A retry over rows on screen does not flash the skeleton")
    @MainActor
    func retryOverRowsKeepsThem() async {
        let gated = GatedNews()
        let model = NewsFeedModel(fetch: { try await gated.read() }, now: { now })
        let first = Task { await model.load() }
        await gated.waitForCalls(1)
        gated.answer(0, with: .success(NewsSampleData.apple(now: now)))
        await first.value

        let second = Task { await model.load() }
        await gated.waitForCalls(2)

        #expect(model.phase == .answered, "rows stay while the second read runs")
        gated.answer(1, with: .success(NewsSampleData.market(now: now)))
        await second.value
        #expect(model.headlines.count == 4)
    }

    @Test("An older read landing last does not overwrite the newer answer")
    @MainActor
    func staleAnswerIsDropped() async {
        let gated = GatedNews()
        let model = NewsFeedModel(fetch: { try await gated.read() }, now: { now })
        let older = Task { await model.load() }
        await gated.waitForCalls(1)
        let newer = Task { await model.refresh() }
        await gated.waitForCalls(2)

        gated.answer(1, with: .success(NewsSampleData.market(now: now)))
        await newer.value
        gated.answer(0, with: .success(NewsSampleData.apple(now: now)))
        await older.value

        #expect(model.headlines.count == 4, "the market answer was issued last and stays")
    }
}
