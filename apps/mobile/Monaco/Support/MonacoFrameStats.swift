#if DEBUG
import QuartzCore
import SwiftUI
import UIKit
import os

/// DEBUG-only render-smoothness meter, off unless the app is launched with `-MonacoFrameStats`.
///
/// A `CADisplayLink` on the main run loop's `.common` modes samples every vsync. A frame that
/// takes longer than its expected duration means the main thread missed one or more display
/// refreshes: those are the frames people see as stutter. We report, per screen:
///
/// - `frames` — display links observed while the screen was on screen
/// - `dropped` — refreshes the main thread missed (sum of `round(actual / expected) - 1`)
/// - `hitches` — frames that took more than 1.5x their budget, i.e. a visible stall
/// - `worst` — longest single frame, in milliseconds
///
/// Numbers are logged through `os.Logger` (category `frame-stats`) so they survive in the
/// device log rather than only the debugger console, and each screen window is wrapped in an
/// os_signpost interval so the same run can be opened in Instruments without new code.
///
/// Attach with `.monacoFrameStats("Home")` at a screen root. No-op in release builds and in
/// debug builds launched without the flag, so it costs nothing in normal use.
enum MonacoFrameStats {
    static let isEnabled = ProcessInfo.processInfo.arguments.contains("-MonacoFrameStats")

    static let log = Logger(
        subsystem: Bundle.main.bundleIdentifier ?? "com.monaco.app",
        category: "frame-stats"
    )

    static let signposter = OSSignposter(
        subsystem: Bundle.main.bundleIdentifier ?? "com.monaco.app",
        category: "frame-stats"
    )
}

/// DEBUG-only launch trace: how long the process spent before the first SwiftUI frame reached
/// the screen. Reads the kernel's recorded process start time, so it covers dyld and every
/// initializer that ran before `main`, not just our own `init`.
enum MonacoLaunchTrace {
    /// Wall-clock moment the kernel started this process.
    static let processStart: Date = {
        var info = kinfo_proc()
        var size = MemoryLayout<kinfo_proc>.stride
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
        let result = sysctl(&mib, u_int(mib.count), &info, &size, nil, 0)
        guard result == 0 else { return Date() }
        let started = info.kp_proc.p_starttime
        return Date(
            timeIntervalSince1970: TimeInterval(started.tv_sec) + TimeInterval(started.tv_usec) / 1_000_000
        )
    }()

    private static var didReportFirstFrame = false

    static func markSceneReady() {
        guard MonacoFrameStats.isEnabled else { return }
        MonacoFrameStats.log.notice(
            "launch scene-ready \(elapsedMilliseconds(), privacy: .public)ms"
        )
    }

    /// Call from the root view's first render; logs once, on the next run-loop turn so the
    /// frame has actually been committed.
    @MainActor
    static func markFirstFrame() {
        guard MonacoFrameStats.isEnabled, !didReportFirstFrame else { return }
        didReportFirstFrame = true
        CATransaction.begin()
        CATransaction.setCompletionBlock {
            MonacoFrameStats.log.notice(
                "launch first-frame \(elapsedMilliseconds(), privacy: .public)ms"
            )
        }
        CATransaction.commit()
    }

    private static func elapsedMilliseconds() -> String {
        String(format: "%.0f", Date().timeIntervalSince(processStart) * 1000)
    }
}

/// One screen's accumulated frame timings.
struct MonacoFrameStatsSample: Equatable {
    var frames = 0
    var droppedFrames = 0
    var hitches = 0
    var worstFrameMilliseconds = 0.0
    var elapsedSeconds = 0.0

    /// Frames actually presented per second over the window.
    var averageFPS: Double {
        elapsedSeconds > 0 ? Double(frames) / elapsedSeconds : 0
    }

    /// Share of expected refreshes that never made it to the screen.
    var droppedPercent: Double {
        let expected = frames + droppedFrames
        return expected > 0 ? Double(droppedFrames) / Double(expected) * 100 : 0
    }

    var summary: String {
        String(
            format: "frames=%d dropped=%d (%.1f%%) hitches=%d worst=%.1fms avgFPS=%.1f over %.1fs",
            frames,
            droppedFrames,
            droppedPercent,
            hitches,
            worstFrameMilliseconds,
            averageFPS,
            elapsedSeconds
        )
    }
}

/// Drives a `CADisplayLink` and accumulates one `MonacoFrameStatsSample`.
@MainActor
final class MonacoFrameStatsRecorder {
    /// A frame that overruns its budget by more than half a refresh is a visible stall.
    private static let hitchThreshold = 1.5

    /// Frames between rolling log lines (~2s at 60Hz) so a scroll can be read off the log
    /// without waiting for the screen to disappear.
    private static let flushInterval = 120

    private var displayLink: CADisplayLink?
    private var lastTimestamp: CFTimeInterval?
    private var sample = MonacoFrameStatsSample()
    private var window = MonacoFrameStatsSample()
    private var screen = ""

    func start(screen: String = "") {
        guard MonacoFrameStats.isEnabled, displayLink == nil else { return }
        self.screen = screen
        sample = MonacoFrameStatsSample()
        window = MonacoFrameStatsSample()
        lastTimestamp = nil
        let link = CADisplayLink(target: self, selector: #selector(tick(_:)))
        // `.common` keeps sampling while a scroll view is tracking a drag — exactly the
        // window we care about; the default mode would stop during scrolling.
        link.add(to: .main, forMode: .common)
        displayLink = link
    }

    /// Stops sampling and returns what was measured, or nil when nothing was.
    @discardableResult
    func stop() -> MonacoFrameStatsSample? {
        guard displayLink != nil else { return nil }
        displayLink?.invalidate()
        displayLink = nil
        lastTimestamp = nil
        return sample.frames > 0 ? sample : nil
    }

    @objc
    private func tick(_ link: CADisplayLink) {
        defer { lastTimestamp = link.timestamp }
        guard let previous = lastTimestamp else { return }
        let expected = link.targetTimestamp - link.timestamp
        let actual = link.timestamp - previous
        record(actual: actual, expected: expected, into: &sample)
        record(actual: actual, expected: expected, into: &window)
        if window.frames >= Self.flushInterval {
            let screen = screen
            let summary = window.summary
            MonacoFrameStats.log.notice(
                "frame-stats window screen=\(screen, privacy: .public) \(summary, privacy: .public)"
            )
            window = MonacoFrameStatsSample()
        }
    }

    private func record(actual: CFTimeInterval, expected: CFTimeInterval, into sample: inout MonacoFrameStatsSample) {
        sample.frames += 1
        sample.elapsedSeconds += actual
        sample.worstFrameMilliseconds = max(sample.worstFrameMilliseconds, actual * 1000)
        guard expected > 0 else { return }
        let refreshes = Int((actual / expected).rounded())
        if refreshes > 1 {
            sample.droppedFrames += refreshes - 1
        }
        if actual > expected * Self.hitchThreshold {
            sample.hitches += 1
        }
    }
}

private struct MonacoFrameStatsModifier: ViewModifier {
    let screen: String

    @State private var recorder = MonacoFrameStatsRecorder()
    @State private var signpostState: OSSignpostIntervalState?

    func body(content: Content) -> some View {
        content
            .onAppear {
                guard MonacoFrameStats.isEnabled else { return }
                signpostState = MonacoFrameStats.signposter.beginInterval(
                    "screen",
                    id: MonacoFrameStats.signposter.makeSignpostID()
                )
                recorder.start(screen: screen)
                MonacoFrameStats.log.notice("frame-stats start screen=\(screen, privacy: .public)")
            }
            .onDisappear {
                guard MonacoFrameStats.isEnabled else { return }
                if let sample = recorder.stop() {
                    MonacoFrameStats.log.notice(
                        "frame-stats screen=\(screen, privacy: .public) \(sample.summary, privacy: .public)"
                    )
                }
                if let signpostState {
                    MonacoFrameStats.signposter.endInterval("screen", signpostState)
                }
                signpostState = nil
            }
    }
}

extension View {
    /// DEBUG frame-time meter for this screen. Inert unless the app was launched with
    /// `-MonacoFrameStats`.
    func monacoFrameStats(_ screen: String) -> some View {
        modifier(MonacoFrameStatsModifier(screen: screen))
    }
}
#else
import SwiftUI

/// No-ops in release builds; the meter exists only in DEBUG.
enum MonacoLaunchTrace {
    static func markSceneReady() {}

    @MainActor
    static func markFirstFrame() {}
}

extension View {
    /// No-op in release builds.
    @inlinable
    func monacoFrameStats(_ screen: String) -> Self { self }
}
#endif
