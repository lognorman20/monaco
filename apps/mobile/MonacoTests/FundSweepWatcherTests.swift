import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// The watch behind Add money's "Added $X to the cabal": it must only say so once the backend
/// has the deposit confirmed, and it must give up on its own rather than poll forever.
struct FundSweepWatcherTests {
    /// Time only moves when the watcher sleeps, so the deadline is exercised without waiting.
    private final class FakeClock: MonotonicClock, @unchecked Sendable {
        var nowSeconds: Double = 1_000
    }

    private struct Dropped: Error {}

    private func watch(
        statuses: [Result<String, Error>],
        maxWait: Duration = .seconds(120),
        interval: Duration = .seconds(3)
    ) async -> (DepositSweepPhase, fetches: Int, sleeps: [Duration]) {
        let clock = FakeClock()
        var remaining = statuses
        var fetches = 0
        var sleeps: [Duration] = []
        let phase = await FundSweepWatcher.watch(
            maxWait: maxWait,
            interval: interval,
            clock: clock,
            sleep: { duration in
                sleeps.append(duration)
                clock.nowSeconds += Double(duration.components.seconds)
            },
            fetchStatus: {
                fetches += 1
                let next = remaining.isEmpty ? Result<String, Error>.success("pending") : remaining.removeFirst()
                return try next.get()
            }
        )
        return (phase, fetches, sleeps)
    }

    @Test func aConfirmedDepositIsCreditedAndTheWatchStops() async {
        let (phase, fetches, _) = await watch(statuses: [.success("pending"), .success("pending"), .success("confirmed")])
        #expect(phase == .credited)
        #expect(fetches == 3)
    }

    /// The backend records a sweep it could not submit as "failed: <reason>".
    @Test func aFailedSweepEndsTheWatchAsAFailure() async {
        let (phase, fetches, _) = await watch(statuses: [.success("pending"), .success("failed: submit_sweep")])
        #expect(phase == .failed("failed: submit_sweep"))
        #expect(fetches == 2)
    }

    /// A poll that did not come back says nothing about the fund. It must not end the watch, and
    /// it must not be read as a failure.
    @Test func aDroppedPollIsNotAFailure() async {
        let (phase, fetches, _) = await watch(statuses: [.failure(Dropped()), .failure(Dropped()), .success("confirmed")])
        #expect(phase == .credited)
        #expect(fetches == 3)
    }

    /// The screen hands a slow sweep over to Activity instead of polling until the member leaves.
    @Test func aSweepThatNeverLandsIsHandedOverAtTheDeadline() async {
        let (phase, fetches, sleeps) = await watch(statuses: [], maxWait: .seconds(12), interval: .seconds(3))
        #expect(phase == .awaitingSweep)
        #expect(fetches == 4)
        #expect(sleeps.allSatisfy { $0 == .seconds(3) })
    }

    @Test func aWatchWhoseScreenHasGoneStopsAtTheNextWait() async {
        let clock = FakeClock()
        var fetches = 0
        let phase = await FundSweepWatcher.watch(
            clock: clock,
            sleep: { _ in throw CancellationError() },
            fetchStatus: {
                fetches += 1
                return "pending"
            }
        )
        #expect(phase == .awaitingSweep)
        #expect(fetches == 1)
    }
}
