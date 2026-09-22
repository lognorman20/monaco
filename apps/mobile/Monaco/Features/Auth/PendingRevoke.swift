import Foundation

/// The best-effort Dynamic revoke a sign-out leaves running, and the bounded wait a new
/// sign-in does on it.
///
/// Sign-out is local-first: the session is cleared before Dynamic is told, so the login screen
/// comes back at once even offline. `sdk.auth.logout()` is an unbounded network call — it is
/// precisely what hangs while offline — so a new sign-in waits for it only briefly and then
/// goes ahead. Waiting without a bound would leave the member on a login form that is
/// disabled for as long as Dynamic takes, with no cancel and no way back but force-quitting;
/// that stall is the whole reason the revoke was moved off the sign-out path.
///
/// Giving up on the wait is safe because the revoke itself checks whether the session it was
/// told to end is still the open one before calling Dynamic.
@MainActor
final class PendingRevoke {
    private var task: Task<Void, Never>?
    private var waiters: [UUID: CheckedContinuation<Void, Never>] = [:]
    /// Identifies the revoke now pending. Sign out, sign in, sign out again and the first
    /// revoke can still be hanging when the second starts; without this the older one's
    /// completion would clear the newer one and release its waiters early.
    private var generation = 0

    var isPending: Bool { task != nil }

    /// Runs `revoke` in the background, releasing anyone waiting once it settles.
    /// Supersedes a revoke that is already pending.
    func start(_ revoke: @escaping @MainActor () async -> Void) {
        generation += 1
        let mine = generation
        task = Task { [self] in
            await revoke()
            finish(mine)
        }
    }

    /// Returns once the revoke settles, or after `timeout`, whichever comes first.
    func wait(atMost timeout: Duration) async {
        guard isPending else { return }
        let id = UUID()
        // Awaiting the task's value cannot be bounded: a `Task<Void, Never>` ignores the
        // awaiting task's cancellation, so racing it inside a task group still blocks until
        // the revoke returns. Wait on a continuation the deadline is able to resume instead.
        let deadline = Task { [self] in
            try? await Task.sleep(for: timeout)
            giveUp(id)
        }
        await withCheckedContinuation { continuation in
            waiters[id] = continuation
        }
        deadline.cancel()
    }

    private func finish(_ generation: Int) {
        // A revoke that has already been superseded settles for itself only: the waiters
        // belong to the one that replaced it.
        guard generation == self.generation else { return }
        task = nil
        let waiting = waiters
        waiters.removeAll()
        for continuation in waiting.values {
            continuation.resume()
        }
    }

    /// Removing the waiter is the once-guard: whichever of the deadline and the revoke
    /// reaches it first is the one that resumes it.
    private func giveUp(_ id: UUID) {
        waiters.removeValue(forKey: id)?.resume()
    }
}
