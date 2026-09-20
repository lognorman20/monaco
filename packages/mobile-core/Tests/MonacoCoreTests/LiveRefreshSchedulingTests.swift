import XCTest
@testable import MonacoCore

/// The scheduling behind "the screen keeps itself fresh": cadence and backoff, pause and resume,
/// one refresh at a time, and a failed poll leaving the screen alone. No real time passes — the
/// loop's sleep is a fake that records the delay it was asked for, and resume is handed the clock
/// readings to use.
final class LiveRefreshSchedulingTests: XCTestCase {

    // MARK: Cadence

    func testCadence_fiveSecondsWhileAProposalIsOpenOrExecuting_fifteenOtherwise() {
        let open = ProposalDTO(id: "p1", symbol: "AAPLx", status: "open")
        let executing = ProposalDTO(id: "p2", symbol: "AAPLx", status: "passed", kind: "buy", execution: ProposalExecutionDTO(state: "pending"))
        let done = ProposalDTO(id: "p3", symbol: "AAPLx", status: "passed", kind: "buy", execution: ProposalExecutionDTO(state: "confirmed"))
        let rejected = ProposalDTO(id: "p4", symbol: "AAPLx", status: "failed")

        XCTAssertEqual(LiveRefreshCadence.watching([done, open]), .seconds(5))
        XCTAssertEqual(LiveRefreshCadence.watching([executing]), .seconds(5))
        XCTAssertEqual(LiveRefreshCadence.watching([done, rejected]), .seconds(15))
        XCTAssertEqual(LiveRefreshCadence.watching([]), .seconds(15))
    }

    // MARK: PollSchedule

    func testSchedule_healthy_staysOnTheBaseInterval() {
        var schedule = PollSchedule(interval: .seconds(3))

        XCTAssertEqual(schedule.currentInterval, .seconds(3))
        schedule.recordSuccess()
        XCTAssertEqual(schedule.currentInterval, .seconds(3))
        XCTAssertEqual(schedule.failureStreak, 0)
    }

    func testSchedule_failuresDoubleTheWait_andStopAtTheCeiling() {
        var schedule = PollSchedule(interval: .seconds(3), maxInterval: .seconds(30))

        schedule.recordFailure()
        XCTAssertEqual(schedule.currentInterval, .seconds(6))
        schedule.recordFailure()
        XCTAssertEqual(schedule.currentInterval, .seconds(12))
        schedule.recordFailure()
        XCTAssertEqual(schedule.currentInterval, .seconds(24))
        schedule.recordFailure()
        XCTAssertEqual(schedule.currentInterval, .seconds(30), "24s doubled is past the cap")

        // A screen left open against a dead backend must not grow an unbounded exponent.
        let streakAtCeiling = schedule.failureStreak
        for _ in 0..<1_000 { schedule.recordFailure() }
        XCTAssertEqual(schedule.failureStreak, streakAtCeiling)
        XCTAssertEqual(schedule.currentInterval, .seconds(30))
    }

    func testSchedule_oneGoodTickClearsTheBackoff() {
        var schedule = PollSchedule(interval: .seconds(5))
        schedule.recordFailure()
        schedule.recordFailure()
        XCTAssertEqual(schedule.currentInterval, .seconds(20))

        schedule.recordSuccess()

        XCTAssertEqual(schedule.currentInterval, .seconds(5))
        XCTAssertEqual(schedule.failureStreak, 0)
    }

    func testSchedule_ceilingBelowTheIntervalIsRaisedToIt() {
        // "Back off" must never mean "poll faster".
        let schedule = PollSchedule(interval: .seconds(10), maxInterval: .seconds(2))

        XCTAssertEqual(schedule.maxInterval, .seconds(10))
        XCTAssertEqual(schedule.currentInterval, .seconds(10))
    }

    // MARK: PollLoop

    func testLoop_waitsBeforeTheFirstTick_soAppearDoesNotDoubleLoad() async {
        let sleeps = SleepRecorder()
        var ticks = 0

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(3))) { delay in
            await sleeps.append(delay)
        } tick: {
            ticks += 1
            return ticks >= 2 ? .stop : .refreshed
        }

        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(3), .seconds(3)])
        XCTAssertEqual(ticks, 2)
    }

    func testLoop_backsOffWhileTicksFail_andRecoversOnTheFirstGoodOne() async {
        let sleeps = SleepRecorder()
        // fail, fail, fail, succeed, then stop.
        var outcomes: [PollTick] = [.failed, .failed, .failed, .refreshed, .stop]

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(5), maxInterval: .seconds(30))) { delay in
            await sleeps.append(delay)
        } tick: {
            outcomes.removeFirst()
        }

        let recorded = await sleeps.values
        XCTAssertEqual(
            recorded,
            [.seconds(5), .seconds(10), .seconds(20), .seconds(30), .seconds(5)],
            "each failure doubles the wait up to the cap; one good tick puts it back on 5s"
        )
    }

    func testLoop_skippedTickIsNotAFailure() async {
        let sleeps = SleepRecorder()
        var outcomes: [PollTick] = [.skipped, .skipped, .stop]

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(4))) { delay in
            await sleeps.append(delay)
        } tick: {
            outcomes.removeFirst()
        }

        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(4), .seconds(4), .seconds(4)], "a dropped tick must not back the loop off")
    }

    func testLoop_stopEndsIt_andReportsTheScheduleItEndedOn() async {
        var outcomes: [PollTick] = [.failed, .stop, .refreshed]
        var ticks = 0

        let ended = await PollLoop.run(schedule: PollSchedule(interval: .seconds(3))) { _ in
        } tick: {
            ticks += 1
            return outcomes.removeFirst()
        }

        XCTAssertEqual(ticks, 2, "nothing runs after .stop")
        XCTAssertEqual(ended.failureStreak, 1)
    }

    func testLoop_endsWhenTheSleepIsCancelled() async {
        var ticks = 0

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(3))) { _ in
            throw CancellationError()
        } tick: {
            ticks += 1
            return .refreshed
        }

        XCTAssertEqual(ticks, 0, "a cancelled wait must not fire a request on the way out")
    }

    func testLoop_endsWhenTheSurroundingTaskIsCancelled() async {
        let ticks = TickCounter()

        let task = Task {
            _ = await PollLoop.run(schedule: PollSchedule(interval: .milliseconds(1))) { delay in
                try await Task.sleep(for: delay)
            } tick: {
                await ticks.increment()
                return .refreshed
            }
        }
        // Let a few ticks through, then cancel and make sure the loop actually unwinds.
        try? await Task.sleep(for: .milliseconds(30))
        task.cancel()
        await task.value

        let afterCancel = await ticks.value
        try? await Task.sleep(for: .milliseconds(30))
        let later = await ticks.value
        XCTAssertEqual(later, afterCancel, "the loop kept running after cancellation")
    }

    // MARK: Pause and resume

    func testResume_firstAppearanceWaitsAFullInterval() {
        let schedule = PollSchedule(interval: .seconds(5))

        XCTAssertEqual(schedule.resumeDelay(lastRefreshSeconds: nil, nowSeconds: 1_000), .seconds(5))
    }

    func testResume_afterALongPauseTicksImmediately() {
        // Backgrounded for a minute: the member must not stare at minute-old votes for 5 more seconds.
        let schedule = PollSchedule(interval: .seconds(5))

        XCTAssertEqual(schedule.resumeDelay(lastRefreshSeconds: 100, nowSeconds: 160), .zero)
        XCTAssertEqual(schedule.resumeDelay(lastRefreshSeconds: 100, nowSeconds: 105), .zero)
    }

    func testResume_afterAShortPauseWaitsOutTheRemainder() {
        // A quick flick to another tab and back must not cost a request per flick.
        let schedule = PollSchedule(interval: .seconds(15))

        XCTAssertEqual(schedule.resumeDelay(lastRefreshSeconds: 100, nowSeconds: 104), .seconds(11))
    }

    func testResume_aClockThatWentBackwardsFallsBackToTheInterval() {
        let schedule = PollSchedule(interval: .seconds(5))

        XCTAssertEqual(schedule.resumeDelay(lastRefreshSeconds: 5_000, nowSeconds: 3), .seconds(5))
    }

    func testLoop_zeroFirstDelayTicksWithoutSleeping_thenKeepsTheCadence() async {
        let sleeps = SleepRecorder()
        var outcomes: [PollTick] = [.refreshed, .refreshed, .stop]

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(5)), firstDelay: .zero) { delay in
            await sleeps.append(delay)
        } tick: {
            outcomes.removeFirst()
        }

        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(5), .seconds(5)], "three ticks, and only the waits between them")
    }

    func testLoop_pausedMidWait_resumesWithTheRemainderAndNoExtraTick() async {
        // Pause: the surrounding task is cancelled during a wait. Resume: a new loop starts with
        // the delay `resumeDelay` hands it.
        var ticks = 0
        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(15))) { _ in
            throw CancellationError()
        } tick: {
            ticks += 1
            return .refreshed
        }
        XCTAssertEqual(ticks, 0)

        let sleeps = SleepRecorder()
        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(15)), firstDelay: .seconds(11)) { delay in
            await sleeps.append(delay)
        } tick: {
            ticks += 1
            return .stop
        }

        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(11)])
        XCTAssertEqual(ticks, 1)
    }

    // MARK: RefreshGate

    @MainActor
    func testGate_dropsARefreshWhileOneIsInFlight() async {
        let gate = RefreshGate()
        let release = RequestLatch()
        var started = 0

        let first = Task { @MainActor in
            await gate.run {
                started += 1
                await release.wait()
            }
        }
        while !gate.isRunning { await Task.yield() }

        let secondRan = await gate.run { started += 1 }
        XCTAssertFalse(secondRan, "a tick that lands mid-refresh must not fire a second request")
        XCTAssertEqual(started, 1)

        await release.open()
        let firstRan = await first.value
        XCTAssertTrue(firstRan)

        let thirdRan = await gate.run { started += 1 }
        XCTAssertTrue(thirdRan, "the gate reopens once the refresh finishes")
        XCTAssertEqual(started, 2)
    }

    @MainActor
    func testGate_aPullToRefreshAlwaysRuns_andHoldsTicksOffWhileItDoes() async {
        let gate = RefreshGate()
        let tickRelease = RequestLatch()
        let pullRelease = RequestLatch()
        let tick = Task { @MainActor in await gate.run { await tickRelease.wait() } }
        while !gate.isRunning { await Task.yield() }

        var pullRan = false
        let pull = Task { @MainActor in
            await gate.runNow {
                pullRan = true
                await pullRelease.wait()
            }
        }
        while !pullRan { await Task.yield() }

        // The tick finishes; the pull is still going, so the gate stays shut.
        await tickRelease.open()
        _ = await tick.value
        XCTAssertTrue(gate.isRunning)
        let tickDuringPull = await gate.run {}
        XCTAssertFalse(tickDuringPull)

        await pullRelease.open()
        await pull.value
        XCTAssertFalse(gate.isRunning)
    }

    @MainActor
    func testGate_reopensAfterAThrowingRefresh() async {
        let gate = RefreshGate()

        do {
            try await gate.run { throw URLError(.notConnectedToInternet) }
            XCTFail("expected the refresh to throw")
        } catch {}

        let ran = await gate.run {}
        XCTAssertTrue(ran, "a failed refresh must not wedge the gate shut")
    }

    @MainActor
    func testGate_throughTheLoop_anOverlappingTickIsSkippedNotFailed() async {
        let gate = RefreshGate()
        let release = RequestLatch()
        let sleeps = SleepRecorder()
        let pull = Task { @MainActor in await gate.run { await release.wait() } }
        while !gate.isRunning { await Task.yield() }

        var ticks = 0
        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(5))) { delay in
            await sleeps.append(delay)
        } tick: {
            ticks += 1
            if ticks == 2 { return .stop }
            return await gate.run {} ? .refreshed : .skipped
        }
        await release.open()
        _ = await pull.value

        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(5), .seconds(5)], "standing down for a pull-to-refresh is not a failure")
    }

    // MARK: Failure keeps the old data

    @MainActor
    func testFailedTick_leavesTheValueOnScreenUntouched_andBacksOff() async {
        // The shape every polled screen has: a value on screen, a fetch that may throw, and a
        // tick that only ever writes what a successful fetch returned.
        var onScreen = ["vote-1"]
        var writes = 0
        var responses: [Result<[String], URLError>] = [
            .failure(URLError(.timedOut)),
            .failure(URLError(.badServerResponse)),
            .success(["vote-1", "vote-2"]),
        ]
        let sleeps = SleepRecorder()
        var seenDuringOutage: [[String]] = []

        _ = await PollLoop.run(schedule: PollSchedule(interval: .seconds(5))) { delay in
            await sleeps.append(delay)
        } tick: {
            guard !responses.isEmpty else { return .stop }
            switch responses.removeFirst() {
            case .success(let fresh):
                QuietUpdate.apply(fresh, over: onScreen) { onScreen = $0; writes += 1 }
                return .refreshed
            case .failure:
                seenDuringOutage.append(onScreen)
                return .failed
            }
        }

        XCTAssertEqual(seenDuringOutage, [["vote-1"], ["vote-1"]], "a failed poll must not blank the screen")
        XCTAssertEqual(onScreen, ["vote-1", "vote-2"])
        XCTAssertEqual(writes, 1)
        let recorded = await sleeps.values
        XCTAssertEqual(recorded, [.seconds(5), .seconds(10), .seconds(20), .seconds(5)])
    }

    func testQuietUpdate_anIdenticalAnswerWritesNothing() {
        // No write means no SwiftUI invalidation: the poll is invisible when nothing changed.
        var onScreen = ["vote-1"]

        XCTAssertFalse(QuietUpdate.apply(["vote-1"], over: onScreen) { onScreen = $0 })
        XCTAssertTrue(QuietUpdate.apply(["vote-1", "vote-2"], over: onScreen) { onScreen = $0 })
        XCTAssertEqual(onScreen, ["vote-1", "vote-2"])
    }

    func testSystemClock_movesForward() async {
        let clock = SystemMonotonicClock()
        let first = clock.nowSeconds
        try? await Task.sleep(for: .milliseconds(20))

        XCTAssertGreaterThan(clock.nowSeconds, first)
    }
}

// MARK: - Helpers

private actor SleepRecorder {
    private(set) var values: [Duration] = []

    func append(_ value: Duration) {
        values.append(value)
    }
}

private actor TickCounter {
    private(set) var value = 0

    func increment() {
        value += 1
    }
}

/// Holds a fake request open until the test lets it finish.
private actor RequestLatch {
    private var isOpen = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func wait() async {
        if isOpen { return }
        await withCheckedContinuation { waiters.append($0) }
    }

    func open() {
        isOpen = true
        waiters.forEach { $0.resume() }
        waiters.removeAll()
    }
}
