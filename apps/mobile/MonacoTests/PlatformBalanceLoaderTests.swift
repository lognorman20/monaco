import Foundation
import Testing
@testable import Monaco

@MainActor
struct PlatformBalanceLoaderTests {
    private func balance(_ micros: Int64) -> PlatformBalanceDTO {
        PlatformBalanceDTO(availableUsdcMicros: micros, memberWalletAddress: "wallet", pendingAllocationMicros: 0)
    }

    /// A box the test can retune between loads.
    private final class Responder {
        var result: Result<PlatformBalanceDTO, Error>
        private(set) var calls = 0

        init(_ result: Result<PlatformBalanceDTO, Error>) { self.result = result }

        func fetch(_ token: String) async throws -> PlatformBalanceDTO {
            calls += 1
            return try result.get()
        }
    }

    /// Holds every request open until the test answers it, so two can be in flight at once.
    private final class SequencedResponder {
        private var pending: [CheckedContinuation<PlatformBalanceDTO, Error>] = []
        private(set) var calls = 0

        func fetch(_ token: String) async throws -> PlatformBalanceDTO {
            calls += 1
            return try await withCheckedThrowingContinuation { pending.append($0) }
        }

        func answer(_ index: Int, with result: Result<PlatformBalanceDTO, Error>) {
            pending[index].resume(with: result)
        }
    }

    /// Lets the loads in flight make progress. Bounded, so a test fails rather than hangs.
    private func settle(until condition: () -> Bool) async {
        for _ in 0 ..< 10_000 {
            if condition() { return }
            await Task.yield()
        }
    }

    @Test func theFirstLoadShowsASpinnerThenTheBalance() async {
        let responder = Responder(.success(balance(5_000_000)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        #expect(loader.phase == .loading)
        await loader.load(accessToken: "token")
        #expect(loader.phase == .loaded(balance(5_000_000)))
    }

    @Test func aFirstLoadThatFailsSaysSoAndOffersARetry() async {
        let responder = Responder(.failure(MonacoAPIError.httpStatus(500)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        await loader.load(accessToken: "token")
        #expect(loader.phase == .failed("Couldn't load your balance."))

        responder.result = .success(balance(1_000_000))
        await loader.load(accessToken: "token")
        #expect(loader.phase == .loaded(balance(1_000_000)))
    }

    /// The bug behind the Add-money freeze: every reload — after a fund, and on the hourly token
    /// rotation — used to swap the amount pad and the bottom button for a spinner, dropping the
    /// keyboard mid-entry. A reload must leave a balance that is already on screen alone.
    @Test func aReloadNeverTakesTheFormAwayFromTheMember() async {
        let responder = Responder(.success(balance(5_000_000)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)
        await loader.load(accessToken: "token")

        responder.result = .failure(MonacoAPIError.httpStatus(503))
        await loader.load(accessToken: "token")

        #expect(loader.phase == .loaded(balance(5_000_000)))
        #expect(loader.failure == nil)
    }

    /// Swallowing every cancellation left `balance` and `failure` both nil, which `phase` reads as
    /// `.loading`: a spinner with no retry, no form and no way off the screen but leaving it.
    /// URLSession returns -999 for more than a cancelled task, so this is not only the
    /// token-rotation case — a cancellation with nothing behind it has to be reported.
    @Test func aCancelledFirstLoadIsNotADeadEnd() async {
        let responder = Responder(.failure(URLError(.cancelled)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        await loader.load(accessToken: "token")

        #expect(loader.phase == .failed("No connection. Check your internet."))
    }

    /// The case the guard was written for: `.task(id: auth.accessToken)` restarts the load when the
    /// token rotates, cancelling the one in flight. That one stays quiet, because the load
    /// replacing it is the one the member is waiting for.
    @Test func aCancellationAnotherLoadIsReplacingStaysQuiet() async {
        let responder = SequencedResponder()
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        async let rotatedOut: Void = loader.load(accessToken: "old-token")
        await settle { responder.calls == 1 }
        async let replacement: Void = loader.load(accessToken: "new-token")
        await settle { responder.calls == 2 }

        responder.answer(0, with: .failure(URLError(.cancelled)))
        await rotatedOut
        #expect(loader.failure == nil)
        #expect(loader.phase == .loading)

        responder.answer(1, with: .success(balance(5_000_000)))
        await replacement
        #expect(loader.phase == .loaded(balance(5_000_000)))
    }

    /// Add money's 3s poll and its reload after a fund overlap. A tick fetched before the money
    /// moved must not land on top of the reload that followed it and put the pre-fund figure back:
    /// both screens derive what the member may spend from this number, so a stale write lets them
    /// submit against a balance that is already gone.
    @Test func aSlowResponseNeverOverwritesANewerOne() async throws {
        let responder = SequencedResponder()
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        // The poll tick goes out first, carrying the pre-fund balance.
        async let polled: PlatformBalanceDTO? = loader.refresh(accessToken: "token")
        await settle { responder.calls == 1 }
        // The reload after the fund goes out second and comes back first.
        async let reloaded: Void = loader.load(accessToken: "token")
        await settle { responder.calls == 2 }

        responder.answer(1, with: .success(balance(1_000_000)))
        await reloaded
        #expect(loader.balance == balance(1_000_000))

        responder.answer(0, with: .success(balance(5_000_000)))
        let announced = try await polled
        // Nothing was written, so there is nothing to announce either.
        #expect(announced == nil)
        #expect(loader.balance == balance(1_000_000))
    }

    /// Signing out clears the screen, and a response already in flight must not put it back.
    @Test func aResponseInFlightDoesNotSurviveSigningOut() async throws {
        let responder = SequencedResponder()
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        async let polled: PlatformBalanceDTO? = loader.refresh(accessToken: "token")
        await settle { responder.calls == 1 }

        await loader.load(accessToken: nil)
        #expect(loader.phase == .failed("Sign in to see your account balance."))

        responder.answer(0, with: .success(balance(5_000_000)))
        _ = try await polled
        #expect(loader.balance == nil)
        #expect(loader.phase == .failed("Sign in to see your account balance."))
    }

    @Test func withoutATokenItAsksTheMemberToSignIn() async {
        let responder = Responder(.success(balance(1)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        await loader.load(accessToken: nil)

        #expect(loader.phase == .failed("Sign in to see your account balance."))
        #expect(responder.calls == 0)
    }

    /// The background poll is silent: it rethrows so the poll loop can back off, and leaves the
    /// screen exactly as the member last saw it.
    @Test func thePollNeverPutsAnErrorOnScreen() async throws {
        let responder = Responder(.success(balance(5_000_000)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)
        await loader.load(accessToken: "token")

        responder.result = .failure(MonacoAPIError.httpStatus(500))
        await #expect(throws: MonacoAPIError.self) {
            try await loader.refresh(accessToken: "token")
        }
        #expect(loader.phase == .loaded(balance(5_000_000)))

        responder.result = .success(balance(9_000_000))
        let fresh = try await loader.refresh(accessToken: "token")
        #expect(fresh == balance(9_000_000))
        #expect(loader.phase == .loaded(balance(9_000_000)))
    }
}
