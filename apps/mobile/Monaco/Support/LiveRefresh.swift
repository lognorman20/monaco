import MonacoCore
import SwiftUI

/// Keeping a screen fresh while the member is looking at it, without anyone writing another timer.
///
/// `pollWhileVisible(every:)` is built on `.task` so SwiftUI owns the lifecycle — the work starts
/// when the view appears and is cancelled when it goes away, with no deinit to remember and no
/// reference to the view to leak. It also goes quiet when the app leaves the foreground and
/// when the member switches to another tab. Coming back picks the cadence up where it left off:
/// an immediate tick if more than one interval went by, the remainder of the wait otherwise.
///
/// The loop is silent about failure: a refresh nobody asked for must never put an error, a
/// spinner or a skeleton in front of the member. Errors belong to the paths they started
/// themselves — pull to refresh, Try again. The scheduling itself (`PollLoop`, `PollSchedule`,
/// `RefreshGate`) lives in MonacoCore, where it is unit tested.

// MARK: - Which tab is on screen

private struct SelectedMainTabKey: EnvironmentKey {
    static let defaultValue: MainTab? = nil
}

private struct HostMainTabKey: EnvironmentKey {
    static let defaultValue: MainTab? = nil
}

extension EnvironmentValues {
    /// The tab the shell is currently showing, or nil outside the tab shell (previews, the
    /// sample harnesses).
    var selectedMainTab: MainTab? {
        get { self[SelectedMainTabKey.self] }
        set { self[SelectedMainTabKey.self] = newValue }
    }

    /// The tab this screen's navigation stack belongs to. Set once per stack by `MainTabView`,
    /// so a screen pushed three levels deep still knows which tab it is in — polling does not
    /// depend on whether SwiftUI tears an unselected tab's tasks down.
    var hostMainTab: MainTab? {
        get { self[HostMainTabKey.self] }
        set { self[HostMainTabKey.self] = newValue }
    }
}

// MARK: - Poll while visible

extension View {
    /// Repeats `action` every `interval` for as long as this view is on screen, `isActive` holds,
    /// its tab is the selected one, and the app is in the foreground.
    ///
    /// A tick that throws is a failed tick: the wait doubles, up to `upTo`, until one succeeds.
    /// Nothing the loop does is ever shown to the member — a tick that fails leaves the screen
    /// exactly as they last saw it.
    ///
    /// - Parameters:
    ///   - interval: the healthy wait between ticks. The first wait happens *before* the first
    ///     tick, because every screen using this has already loaded on appear. A loop coming
    ///     back from a pause ticks as soon as a full interval has passed since its last refresh.
    ///   - upTo: the longest the loop ever waits once ticks start failing.
    ///   - isActive: false parks the loop — a proposal that closed, a screen with nothing left
    ///     to watch. Flipping it cancels or restarts the task.
    ///   - gate: share the screen's gate to have ticks stand down during a pull-to-refresh.
    ///     Omit it and the loop gets its own, which is enough to keep ticks from overlapping
    ///     each other.
    func pollWhileVisible(
        every interval: Duration,
        upTo maxInterval: Duration = .seconds(30),
        isActive: Bool = true,
        gate: RefreshGate? = nil,
        action: @escaping () async throws -> Void
    ) -> some View {
        modifier(
            PollWhileVisible(
                interval: interval,
                maxInterval: maxInterval,
                isActive: isActive,
                sharedGate: gate,
                action: action
            )
        )
    }
}

private struct PollWhileVisible: ViewModifier {
    let interval: Duration
    let maxInterval: Duration
    let isActive: Bool
    let sharedGate: RefreshGate?
    let action: () async throws -> Void

    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.selectedMainTab) private var selectedMainTab
    @Environment(\.hostMainTab) private var hostMainTab
    @State private var ownGate = RefreshGate()
    @State private var memory = PollMemory()

    private let clock: MonotonicClock = SystemMonotonicClock()

    /// When the last good tick landed. A reference so that recording it never invalidates the view.
    @MainActor
    private final class PollMemory {
        var lastRefreshSeconds: Double?
    }

    /// Outside the tab shell there is no selection to compare against, so the screen counts as
    /// selected — previews and the sample harnesses behave as they always did.
    private var isTabSelected: Bool {
        guard let hostMainTab, let selectedMainTab else { return true }
        return hostMainTab == selectedMainTab
    }

    /// Everything that should stop or restart the loop, in one value. `.task(id:)` cancels the
    /// running loop whenever it changes — and on disappear — which is the whole lifecycle:
    /// leaving the screen, backgrounding the app, and the screen going quiet all land here.
    private struct Lifecycle: Equatable {
        let isActive: Bool
        let isForeground: Bool
        let interval: Duration
    }

    func body(content: Content) -> some View {
        let isRunnable = isActive && isTabSelected
        return content.task(
            id: Lifecycle(isActive: isRunnable, isForeground: scenePhase == .active, interval: interval)
        ) {
            guard isRunnable, scenePhase == .active else { return }
            let gate = sharedGate ?? ownGate
            // First start: the screen has just loaded itself, which counts as its last refresh.
            if memory.lastRefreshSeconds == nil { memory.lastRefreshSeconds = clock.nowSeconds }
            let schedule = PollSchedule(interval: interval, maxInterval: maxInterval)
            _ = await PollLoop.run(
                schedule: schedule,
                firstDelay: schedule.resumeDelay(
                    lastRefreshSeconds: memory.lastRefreshSeconds,
                    nowSeconds: clock.nowSeconds
                )
            ) { delay in
                try await Task.sleep(for: delay)
            } tick: {
                await tick(through: gate)
            }
        }
    }

    private func tick(through gate: RefreshGate) async -> PollTick {
        do {
            guard try await gate.run(action) else { return .skipped }
            memory.lastRefreshSeconds = clock.nowSeconds
            return .refreshed
        } catch {
            if Task.isCancelled || error is CancellationError { return .stop }
            // The request was dropped rather than answered (a navigation cancelled it, say).
            // Nothing is wrong with the server, so don't back off.
            if error.isRequestCancellation { return .skipped }
            return .failed
        }
    }
}
