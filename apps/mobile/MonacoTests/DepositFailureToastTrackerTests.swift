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

    /// Opening another cabal used to prune this one's states entirely, so coming back read every
    /// deposit as first-seen — and a fund that failed while the member was elsewhere was filed as
    /// history and never announced.
    @Test func aFailureThatHappensWhileAnotherCabalIsOpenIsStillAnnounced() {
        DepositFailureToastTracker.resetForTesting()
        // Cabal A, one fund on its way.
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("a1", "pending")]).isEmpty)
        // The member opens cabal B while it is in flight.
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("b1", "pending")]).isEmpty)
        // Back to A, where it has since failed.
        #expect(DepositFailureToastTracker.consumeNewFailures(from: [deposit("a1", "failed")]).map(\.id) == ["a1"])
    }

    /// Toast copy: one short sentence, no em dash, no internal full stop, money through the one
    /// formatter. `MonacoToastBanner` states the rule.
    @Test func theMessageSpellsTheAmountLikeEveryOtherAmount() {
        let message = DepositFailureToastTracker.message(for: deposit("d1", "failed", micros: 1_250_500_000))
        #expect(message == "Couldn't add $1,250.50 to the cabal")
        #expect(!message.contains("—"))
        // One sentence: no full stop ending it, and none breaking it in two either. The dot in
        // "$1,250.50" is a decimal point, so the check is for a stop followed by a space.
        #expect(!message.hasSuffix("."))
        #expect(!message.contains(". "))
    }
}
