import Foundation
import MonacoCore

/// Announces a fund that fails *while the member is watching it*.
///
/// The tracker used to remember which ids it had toasted, which made "already failed when the
/// screen opened" indistinguishable from "just failed": every cold start, the first load of a
/// cabal re-announced failures from days ago, in red, over an Activity list that already showed
/// them. It also marked every failure in a batch as consumed while the caller only read the
/// first, so a second failure was silently swallowed.
///
/// So it tracks status instead of ids: a deposit is announced when the screen saw it in some other
/// state first and sees it failed now. A deposit that is already failed the first time this screen
/// lays eyes on it is history, not news. At most one failure is handed over per call — the rest
/// keep their previous status and come round on the next poll, so none are lost.
enum DepositFailureToastTracker {
    private static let failed = "failed"
    /// Deposit id → the state this screen last saw it in. Pruned to what is on screen, so it
    /// cannot grow without bound as cabals are opened.
    private static var lastSeenState: [String: String] = [:]

    static func consumeNewFailures(from items: [GroupActivityItemDTO]) -> [GroupActivityItemDTO] {
        let deposits = items.filter { $0.kind.lowercased() == "deposit" }
        var nextState: [String: String] = [:]
        var announced: GroupActivityItemDTO?

        for item in deposits {
            let state = state(of: item)
            let previous = lastSeenState[item.id]
            let justFailed = previous != nil && previous != failed && state == failed
            if justFailed, announced == nil {
                announced = item
                nextState[item.id] = state
            } else if justFailed {
                // Hold the old state so the next poll still sees the change and announces it.
                nextState[item.id] = previous
            } else {
                nextState[item.id] = state
            }
        }

        lastSeenState = nextState
        return announced.map { [$0] } ?? []
    }

    static func message(for item: GroupActivityItemDTO) -> String {
        "Fund failed. \(UsdAmountFormatter.format(micros: item.amountMicros)) didn't reach the cabal"
    }

    /// The three states worth telling apart, from the many strings the backend sends
    /// ("failed: submit_sweep", "", an unknown word): anything that is not pending or confirmed
    /// and reads as a failure is a failure, and anything else is simply "some other state".
    private static func state(of item: GroupActivityItemDTO) -> String {
        let status = item.status.trimmingCharacters(in: .whitespacesAndNewlines)
        if DepositStatusNormalizer.isPending(status) { return "pending" }
        if DepositStatusNormalizer.isConfirmed(status) { return "confirmed" }
        if DepositStatusNormalizer.isFailed(status) { return failed }
        return status.lowercased()
    }

    /// Tests drive a fresh screen; the process-wide memory must not leak between them.
    static func resetForTesting() {
        lastSeenState = [:]
    }
}
