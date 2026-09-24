import Foundation
import Testing
@testable import Monaco

/// Signing out hands Privy's `user.logout()` to a background revoke. A new sign-in waits for
/// it so a late revoke cannot tear down the session it is creating — but Privy's logout is
/// the unbounded network call that hangs while offline, so the wait has to give up.
///
/// Without the bound: sign out on a bad connection, tap "Send code" a second later, and the
/// login form sits disabled behind a revoke that may never return, with no cancel and no way
/// back but force-quitting the app.
@MainActor
struct PendingRevokeTests {
    /// Long enough that a wait which is not bounded cannot possibly pass this test.
    private static let neverReturns = Duration.seconds(30)

    private func secondsElapsed(_ body: () async -> Void) async -> Double {
        let start = ContinuousClock.now
        await body()
        let elapsed = ContinuousClock.now - start
        return Double(elapsed.components.seconds)
            + Double(elapsed.components.attoseconds) / 1e18
    }

    @Test func aWaitGivesUpOnARevokeThatNeverReturns() async {
        let revoke = PendingRevoke()
        revoke.start {
            try? await Task.sleep(for: Self.neverReturns)
        }

        let elapsed = await secondsElapsed {
            await revoke.wait(atMost: .milliseconds(50))
        }

        // Generous headroom over the 50ms bound: the failure this guards against is a wait
        // that blocks for the revoke's full 30s, so the margin does not make it flaky.
        #expect(elapsed < 5)
    }

    @Test func aWaitEndsAsSoonAsTheRevokeFinishes() async {
        let revoke = PendingRevoke()
        revoke.start {}

        let elapsed = await secondsElapsed {
            await revoke.wait(atMost: .seconds(30))
        }

        // Returns on the revoke finishing, not on the timeout.
        #expect(elapsed < 5)
        #expect(!revoke.isPending)
    }

    @Test func nothingToWaitForReturnsAtOnce() async {
        let revoke = PendingRevoke()

        let elapsed = await secondsElapsed {
            await revoke.wait(atMost: .seconds(30))
        }

        #expect(elapsed < 5)
        #expect(!revoke.isPending)
    }

    /// Both a resend and a verify can be waiting on the same revoke.
    @Test func everyWaiterIsReleasedWhenTheRevokeFinishes() async {
        let revoke = PendingRevoke()
        let release = SignallingGate()
        revoke.start { await release.wait() }

        async let first: Void = revoke.wait(atMost: .seconds(30))
        async let second: Void = revoke.wait(atMost: .seconds(30))
        await Task.yield()
        release.open()
        _ = await (first, second)

        #expect(!revoke.isPending)
    }

    /// Sign out, sign in, sign out again: the first revoke can still be hanging when the
    /// second starts. The older one settling must not be mistaken for the newer one, or a
    /// sign-in would stop waiting for a revoke that is still about to run.
    @Test func anOlderRevokeSettlingDoesNotRetireTheOneThatReplacedIt() async {
        let revoke = PendingRevoke()
        let first = SignallingGate()
        let second = SignallingGate()

        revoke.start { await first.wait() }
        revoke.start { await second.wait() }

        // The superseded revoke finally returns.
        first.open()
        await Task.yield()
        #expect(revoke.isPending)

        second.open()
        await revoke.wait(atMost: .seconds(30))
        #expect(!revoke.isPending)
    }

    /// A revoke that outlives its wait must still release cleanly rather than resuming a
    /// continuation the deadline already took.
    @Test func aRevokeThatFinishesAfterTheWaitGaveUpIsHarmless() async {
        let revoke = PendingRevoke()
        let release = SignallingGate()
        revoke.start { await release.wait() }

        await revoke.wait(atMost: .milliseconds(50))
        release.open()
        // Settles without tripping the checked-continuation double-resume trap.
        await revoke.wait(atMost: .milliseconds(50))
    }
}

/// A gate a test opens by hand, so a revoke can be held without sleeping for a guessed time.
@MainActor
private final class SignallingGate {
    private var continuation: CheckedContinuation<Void, Never>?
    private var isOpen = false

    func wait() async {
        guard !isOpen else { return }
        await withCheckedContinuation { self.continuation = $0 }
    }

    func open() {
        isOpen = true
        continuation?.resume()
        continuation = nil
    }
}
