import Foundation
import Testing
@testable import Monaco

/// The tracker keeps one memory for the process, so these run one at a time.
@Suite(.serialized)
struct DepositFailureToastTrackerTests {
    private func deposit(_ id: String, _ status: String, micros: Int64 = 12_500_000) -> GroupActivityItemDTO {
        GroupActivityItemDTO(
            id: id,
            kind: "deposit",
            status: status,
            symbol: nil,
            amountMicros: micros,
            createdAt: "2026-09-18T15:04:05Z",
            txSignature: nil,
            tokenAmount: nil,
            proceedsUsdcMicros: nil,
            initiatedBy: nil,
            agentDisplayName: nil
        )
    }

    /// The bug: opening a cabal after a cold start greeted the member with a red toast about a
    /// deposit that failed days ago and is already sitting in Activity.
    @Test func aFailureThatIsAlreadyHistoryIsNotAnnounced() {
        DepositFailureToastTracker.resetForTesting()
        let feed = [deposit("old", "failed"), deposit("older", "failed: submit_sweep")]

        #expect(DepositFailureToastTracker.consumeNewFailures(from: feed).isEmpty)
        // And it stays quiet on every later poll.
        #expect(DepositFailureToastTracker.consumeNewFailures(from: feed).isEmpty)
    }

    @Test func aDepositThatFailsWhileTheScreenWatchesIsAnnounced() {
        DepositFailureToastTracker.resetForTesting()
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("d1", "pending")]).isEmpty)

        let announced = DepositFailureToastTracker.consumeNewFailures(from: [deposit("d1", "failed")])
        #expect(announced.map(\.id) == ["d1"])
        // Once only.
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("d1", "failed")]).isEmpty)
    }

    /// The caller shows one toast at a time, so a second failure must wait rather than be
    /// marked as seen and dropped.
    @Test func aSecondFailureInTheSameBatchComesRoundOnTheNextPoll() {
        DepositFailureToastTracker.resetForTesting()
        let pending = [deposit("d1", "pending"), deposit("d2", "pending")]
        #expect(DepositFailureToastTracker.consumeNewFailures(from: pending).isEmpty)

        let bothFailed = [deposit("d1", "failed"), deposit("d2", "failed")]
        #expect(DepositFailureToastTracker.consumeNewFailures(from: bothFailed).map(\.id) == ["d1"])
        #expect(DepositFailureToastTracker.consumeNewFailures(from: bothFailed).map(\.id) == ["d2"])
        #expect(DepositFailureToastTracker.consumeNewFailures(from: bothFailed).isEmpty)
    }

    @Test func aDepositThatLandsIsNeverAnnounced() {
        DepositFailureToastTracker.resetForTesting()
        _ = DepositFailureToastTracker.consumeNewFailures(from: [deposit("d1", "pending")])
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("d1", "confirmed")]).isEmpty)
    }

    @Test func otherKindsOfActivityAreNotDeposits() {
        DepositFailureToastTracker.resetForTesting()
        let trade = GroupActivityItemDTO(
            id: "t1",
            kind: "trade",
            status: "failed",
            symbol: "SOL",
            amountMicros: 1_000_000,
            createdAt: "2026-09-18T15:04:05Z",
            txSignature: nil,
            tokenAmount: nil,
            proceedsUsdcMicros: nil,
            initiatedBy: nil,
            agentDisplayName: nil
        )
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [trade]).isEmpty)
    }

    /// Toast copy: a short sentence, no em dash, money through the one formatter.
    @Test func theMessageSpellsTheAmountLikeEveryOtherAmount() {
        let message = DepositFailureToastTracker.message(for: deposit("d1", "failed", micros: 1_250_500_000))
        #expect(message == "Fund failed. $1,250.50 didn't reach the cabal")
        #expect(!message.contains("—"))
    }
}
