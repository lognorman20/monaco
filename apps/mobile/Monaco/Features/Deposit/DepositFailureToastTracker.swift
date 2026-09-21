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
    /// Deposit id → the state this screen last saw it in. Ids are unique across cabals, so one map
    /// serves them all.
    ///
    /// It is deliberately *not* rebuilt from the batch in hand. Pruning to the items just passed
    /// meant opening cabal B dropped everything known about cabal A: coming back to A, every
    /// deposit looked first-seen, and a fund that failed while the member was away was read as
    /// history and never announced. Entries are kept in the order they were first seen and the
    /// oldest go once there are more than `capacity`, which is what bounds the map instead.
    private static var lastSeenState: [String: FundStatus] = [:]
    private static var firstSeenOrder: [String] = []
    private static let capacity = 500

    static func consumeNewFailures(from items: [GroupActivityItemDTO]) -> [GroupActivityItemDTO] {
        let deposits = items.filter { $0.kind.lowercased() == "deposit" }
        var announced: GroupActivityItemDTO?

        for item in deposits {
            let state = FundStatus(item.status)
            let previous = lastSeenState[item.id]
            let justFailed = previous != nil && previous != .failed && state == .failed
            if justFailed, announced != nil {
                // Leave the old state in place so the next poll still sees the change and
                // announces it. The caller only shows one toast at a time.
                continue
            }
            if justFailed { announced = item }
            record(item.id, as: state)
        }

        return announced.map { [$0] } ?? []
    }

    private static func record(_ id: String, as state: FundStatus) {
        guard lastSeenState.updateValue(state, forKey: id) == nil else { return }
        firstSeenOrder.append(id)
        guard firstSeenOrder.count > capacity else { return }
        lastSeenState.removeValue(forKey: firstSeenOrder.removeFirst())
    }

    static func message(for item: GroupActivityItemDTO) -> String {
        "Couldn't add \(UsdAmountFormatter.format(micros: item.amountMicros)) to the cabal"
    }

    #if DEBUG
    /// Tests drive a fresh screen; the process-wide memory must not leak between them.
    static func resetForTesting() {
        lastSeenState = [:]
        firstSeenOrder = []
    }
    #endif
}
