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

    /// `.task(id: auth.accessToken)` restarts the load whenever the token rotates, cancelling the
    /// request in flight. That used to be reported as "No connection".
    @Test func aCancelledRequestIsNotAFailure() async {
        let responder = Responder(.failure(URLError(.cancelled)))
        let loader = PlatformBalanceLoader(fetch: responder.fetch)

        await loader.load(accessToken: "token")

        #expect(loader.phase == .loading)
        #expect(loader.failure == nil)
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
