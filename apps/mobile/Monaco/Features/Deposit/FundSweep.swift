import Foundation
import MonacoCore

/// How often Add money looks again while the member is on it.
enum AddMoneyPolling {
    /// The account balance, while Deposit or Add money is open, so arriving USDC shows quickly.
    static let balanceInterval: Duration = .seconds(3)
    /// One fund's status, while the screen that made it is watching its sweep into the pot.
    static let sweepStatusInterval: Duration = .seconds(3)
    /// How long a screen watches one sweep before handing it over to the cabal's Activity. The
    /// backend keeps sweeping either way; this only bounds how long the screen holds on.
    static let sweepMaxWait: Duration = .seconds(120)
}

/// The deposit states worth telling apart, from the strings the backend sends.
///
/// A fund is `pending` until the sweep lands, then `confirmed`; a sweep that could not go through
/// is `failed`, or `failed: <reason>` (`postgres.MarkDepositFailed`). Anything else is kept as it
/// came, lowercased, so a caller can tell "some other state" from the three it knows.
enum FundStatus: Equatable {
    case pending
    case confirmed
    case failed
    case other(String)

    init(_ raw: String) {
        let status = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        switch status {
        case "pending":
            self = .pending
        case "confirmed":
            self = .confirmed
        case "failed":
            self = .failed
        default:
            self = status.hasPrefix("failed:") ? .failed : .other(status)
        }
    }
}

/// Watches one fund until the pot has it, it fails, or the screen stops caring.
///
/// `POST /v1/groups/{id}/fund` only records the intent; the sweep from the member wallet into the
/// treasury happens afterwards. So the fund call succeeding is not "added" — this is what finds
/// out. It is a plain function of its inputs so the loop can be tested without a clock or a
/// network, and the caller runs it from `.task(id:)` so it stops with the screen.
enum FundSweepWatcher {
    static func watch(
        maxWait: Duration = AddMoneyPolling.sweepMaxWait,
        interval: Duration = AddMoneyPolling.sweepStatusInterval,
        clock: any MonotonicClock = SystemMonotonicClock(),
        sleep: (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        fetchStatus: () async throws -> String
    ) async -> DepositSweepPhase {
        var machine = DepositPollStateMachine()
        let deadline = clock.nowSeconds + seconds(maxWait)
        while clock.nowSeconds < deadline {
            if Task.isCancelled { return machine.phase }
            do {
                machine.apply(status: try await fetchStatus())
                if machine.isTerminal { return machine.phase }
            } catch {
                // A dropped poll says nothing about the fund. Keep the last known phase and look
                // again on the next tick.
            }
            do {
                try await sleep(interval)
            } catch {
                // Cancelled while waiting: the screen has gone.
                return machine.phase
            }
        }
        return machine.phase
    }

    private static func seconds(_ duration: Duration) -> Double {
        let parts = duration.components
        return Double(parts.seconds) + Double(parts.attoseconds) / 1e18
    }
}
