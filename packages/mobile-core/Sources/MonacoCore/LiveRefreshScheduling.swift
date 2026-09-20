import Foundation

/// Scheduling for screens that keep themselves fresh while the member is looking at them:
/// how long to wait between polls, how far to back off when the server stops answering, how
/// soon a screen coming back into view is worth a request, and the two rules every tick obeys —
/// one refresh at a time, and never touch a value the server did not change.
///
/// Everything here is a pure value type with no clock of its own — the caller owns the waiting.
/// That is what lets `LiveRefreshSchedulingTests` drive thousands of ticks on the host in
/// milliseconds instead of asserting against real timers in the simulator.

// MARK: - Cadence

/// How often a visible screen re-reads its data.
public enum LiveRefreshCadence {
    /// Something is in play — a proposal collecting votes, a swap executing — and another
    /// member's tap should show up while the member is still looking.
    public static let inPlay: Duration = .seconds(5)
    /// Nothing is moving: balances, pot values, and noticing that a new proposal arrived.
    public static let resting: Duration = .seconds(15)

    /// `inPlay` while any of `proposals` is open for voting or waiting on its swap.
    public static func watching(_ proposals: [ProposalDTO]) -> Duration {
        proposals.contains { $0.isOpen || $0.isAwaitingExecution } ? inPlay : resting
    }
}

// MARK: - Poll schedule

/// The wait between ticks of a visible-screen poll, with exponential backoff after failures.
public struct PollSchedule: Equatable, Sendable {
    /// The wait between ticks while the server is answering.
    public let interval: Duration
    /// The longest this schedule ever waits, however many ticks in a row have failed.
    public let maxInterval: Duration
    /// Ticks that have failed since the last good one.
    public private(set) var failureStreak: Int

    public init(interval: Duration, maxInterval: Duration = .seconds(30)) {
        precondition(interval > .zero, "a poll interval must be positive")
        self.interval = interval
        // A cap below the healthy interval would mean "back off by polling faster".
        self.maxInterval = Swift.max(maxInterval, interval)
        self.failureStreak = 0
    }

    /// `interval` doubled once per consecutive failure, never past `maxInterval`. A screen left
    /// open against a backend that is down settles at one request every 30 seconds instead of
    /// hammering it at the healthy rate.
    public var currentInterval: Duration {
        guard failureStreak > 0 else { return interval }
        var delay = interval
        for _ in 0..<failureStreak {
            // Doubling by addition: `failureStreak` is capped below, so this cannot run away.
            delay += delay
            if delay >= maxInterval { return maxInterval }
        }
        return delay
    }

    /// The server answered: back to the healthy interval.
    public mutating func recordSuccess() {
        failureStreak = 0
    }

    /// One more failed tick. The streak stops growing once the wait is already at the cap, so a
    /// screen left open for hours cannot accumulate an unbounded exponent.
    public mutating func recordFailure() {
        guard currentInterval < maxInterval else { return }
        failureStreak += 1
    }
}

// MARK: - Poll loop

/// What one tick of a poll loop did. Only a real failure makes the loop back off.
public enum PollTick: Equatable, Sendable {
    /// The work ran and the screen has fresh data.
    case refreshed
    /// The work ran and failed. The loop waits longer before trying again.
    case failed
    /// Nothing ran, because a refresh was already in flight. Neither progress nor a reason to
    /// back off — the next tick comes at the healthy interval.
    case skipped
    /// Stop polling: the screen reached a terminal state, or the task was cancelled.
    case stop
}

/// The body of a visible-screen poll: wait, tick, adjust the wait, repeat — until a tick says
/// stop or the surrounding task is cancelled.
///
/// Sequencing only. The caller injects both the waiting and the work, so the loop that ships in
/// `pollWhileVisible` is the same loop the unit tests exercise with a fake sleep.
///
/// The first thing it does is wait: every screen that uses this has already loaded its data on
/// appear, so an immediate tick would just repeat that request. A loop that is resuming passes
/// `firstDelay` (see `PollSchedule.resumeDelay`) to shorten or skip that first wait.
public enum PollLoop {
    /// Runs until cancelled or told to stop. Returns the schedule it ended on, which is what the
    /// tests assert against.
    @discardableResult
    public static func run(
        schedule: PollSchedule,
        firstDelay: Duration? = nil,
        sleep: (Duration) async throws -> Void,
        tick: () async -> PollTick
    ) async -> PollSchedule {
        var schedule = schedule
        var delay = firstDelay ?? schedule.currentInterval
        while !Task.isCancelled {
            do {
                // A zero wait is a loop picking up where it left off: tick straight away.
                if delay > .zero { try await sleep(delay) }
            } catch {
                // Cancelled mid-wait, or the caller's sleep gave up. Either way, stop.
                return schedule
            }
            if Task.isCancelled { return schedule }
            switch await tick() {
            case .refreshed:
                schedule.recordSuccess()
            case .failed:
                schedule.recordFailure()
            case .skipped:
                break
            case .stop:
                return schedule
            }
            delay = schedule.currentInterval
        }
        return schedule
    }
}

// MARK: - Picking the loop back up

/// A seconds counter that only ever goes forward. Production reads process uptime rather than
/// the wall clock, which the user or the network can move backwards or forwards by hours — a
/// wall-clock reading would make a resumed loop think no time, or all the time, had passed.
public protocol MonotonicClock: Sendable {
    var nowSeconds: Double { get }
}

public struct SystemMonotonicClock: MonotonicClock {
    public init() {}

    public var nowSeconds: Double { ProcessInfo.processInfo.systemUptime }
}

extension PollSchedule {
    /// How long a loop that is starting — or starting again after the app was backgrounded, the
    /// tab was switched away, or another screen was pushed on top — waits before its first tick.
    ///
    /// `lastRefreshSeconds` is nil the first time a screen appears: it has just loaded itself, so
    /// the loop waits a full interval rather than repeating that request. Coming back later, the
    /// time spent away counts: after more than one interval the first tick is immediate, so the
    /// member never looks at numbers older than the cadence promises.
    public func resumeDelay(lastRefreshSeconds: Double?, nowSeconds: Double) -> Duration {
        guard let lastRefreshSeconds, nowSeconds >= lastRefreshSeconds else { return interval }
        let remaining = interval.inSeconds - (nowSeconds - lastRefreshSeconds)
        guard remaining > 0 else { return .zero }
        return .milliseconds(Int64((remaining * 1_000).rounded()))
    }
}

// MARK: - One refresh at a time

/// Serializes one screen's refreshes. A poll tick and a pull-to-refresh both go through the
/// gate, so a tick that lands while the member is already refreshing is dropped instead of
/// firing a second identical request and racing its own answer back into the view.
@MainActor
public final class RefreshGate {
    private var inFlight = 0

    public var isRunning: Bool { inFlight > 0 }

    public init() {}

    /// Runs `work` unless a refresh is already in flight. Returns whether it ran. For ticks.
    @discardableResult
    public func run(_ work: () async throws -> Void) async rethrows -> Bool {
        guard !isRunning else { return false }
        try await runNow(work)
        return true
    }

    /// Runs `work` regardless, holding the gate shut while it does. For a refresh the member
    /// asked for: a pull must never be dropped because a tick happened to be in flight.
    public func runNow(_ work: () async throws -> Void) async rethrows {
        inFlight += 1
        defer { inFlight -= 1 }
        try await work()
    }
}

// MARK: - Quiet updates

/// What a background poll is allowed to do to a value the member is already looking at: replace
/// it when the server sent something different, and otherwise leave it alone. Assigning an equal
/// value would still invalidate the SwiftUI views reading it, and a failed poll has nothing to
/// assign at all — the old value stays exactly as it was.
public enum QuietUpdate {
    /// Calls `write` with `fresh` only when it differs from `current`. Returns whether it did.
    ///
    /// A closure rather than `inout`: an `inout` access to an `@Observable` property counts as a
    /// mutation whether or not the value changes, which is exactly the invalidation to avoid.
    @discardableResult
    public static func apply<Value: Equatable>(
        _ fresh: Value,
        over current: Value,
        write: (Value) -> Void
    ) -> Bool {
        guard fresh != current else { return false }
        write(fresh)
        return true
    }
}

extension Duration {
    /// Seconds as a `Double`. `components` is (seconds, attoseconds).
    var inSeconds: Double {
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    }
}
