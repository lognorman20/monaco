import SwiftUI
import Testing
@testable import Monaco

struct MonacoToastTimingTests {
    @Test func shortSuccessKeepsTheOldDwell() {
        let dwell = MonacoToastTiming.dwell(message: "Address copied", isSuccess: true, voiceOverRunning: false)
        #expect(dwell >= 2.5)
        #expect(dwell <= 3.0)
    }

    @Test func aMoneyFailureStaysUpLongEnoughToRead() {
        let message = "We couldn't confirm that went through. Check your balance before trying again."
        let dwell = MonacoToastTiming.dwell(message: message, isSuccess: false, voiceOverRunning: false)
        // 77 characters at the old fixed 2.5s was about 18 words a second.
        #expect(dwell > 2.5)
        #expect(dwell >= 6)
    }

    @Test func everyFailureClearsTheFailureFloor() {
        let dwell = MonacoToastTiming.dwell(message: "No", isSuccess: false, voiceOverRunning: false)
        #expect(dwell == MonacoToastTiming.failureFloor)
    }

    @Test func dwellIsCappedSoAToastNeverSticks() {
        let message = String(repeating: "a", count: 4000)
        #expect(MonacoToastTiming.dwell(message: message, isSuccess: false, voiceOverRunning: false)
            == MonacoToastTiming.ceiling)
        #expect(MonacoToastTiming.dwell(message: message, isSuccess: false, voiceOverRunning: true)
            == MonacoToastTiming.voiceOverCeiling)
    }

    @Test func voiceOverGetsMoreTime() {
        let message = "Couldn't add that money. Check your connection and try again."
        let spoken = MonacoToastTiming.dwell(message: message, isSuccess: false, voiceOverRunning: true)
        let silent = MonacoToastTiming.dwell(message: message, isSuccess: false, voiceOverRunning: false)
        #expect(spoken > silent)
    }

    /// An empty message is a caller bug, but it must not produce a zero-length or negative dwell —
    /// the banner would flash and go, and `dismiss` would be handed a dwell it can never satisfy.
    @Test func anEmptyMessageStillGetsItsFloor() {
        let success = MonacoToastTiming.dwell(message: "", isSuccess: true, voiceOverRunning: false)
        let failure = MonacoToastTiming.dwell(message: "", isSuccess: false, voiceOverRunning: false)
        #expect(success == MonacoToastTiming.successFloor)
        #expect(failure == MonacoToastTiming.failureFloor)
        #expect(success > 0)
    }
}

/// The dwell countdown, and in particular the hold that used to be unbounded.
struct MonacoToastCountdownTests {
    private let slice: TimeInterval = 0.1

    /// Runs the countdown the way `dismiss` does, with a cap so a hang fails the test
    /// instead of hanging the suite. Returns the wall-clock time it took.
    private func run(
        _ countdown: inout MonacoToastCountdown,
        isHeld: @escaping (Int) -> Bool,
        limitTicks: Int = 10_000
    ) -> (elapsed: TimeInterval, finished: Bool) {
        var ticks = 0
        while !countdown.isFinished, ticks < limitTicks {
            countdown.tick(slice: slice, isHeld: isHeld(ticks))
            ticks += 1
        }
        return (Double(ticks) * slice, countdown.isFinished)
    }

    @Test func anUntouchedToastGoesAfterItsDwell() {
        var countdown = MonacoToastCountdown(dwell: 3)
        let result = run(&countdown, isHeld: { _ in false })
        #expect(result.finished)
        #expect(abs(result.elapsed - 3) < 0.15)
    }

    /// The regression. A gesture that is cancelled rather than ended — the banner torn down
    /// mid-drag, the screen pushed away, a system edge gesture winning — never delivers
    /// `onEnded`, so the hold flag stays set. The old loop only ever decremented while the flag
    /// was clear, so it slept at 10 Hz forever and pinned the toast over the money screen with
    /// tap as the only way out. The wall-clock ceiling is what ends it.
    @Test func aHoldThatNeverEndsStillReleasesTheToast() {
        let dwell: TimeInterval = 4
        var countdown = MonacoToastCountdown(dwell: dwell)
        let result = run(&countdown, isHeld: { _ in true })

        #expect(result.finished, "a toast held by a cancelled gesture never came down")
        #expect(abs(result.elapsed - (dwell + MonacoToastTiming.maximumHold)) < 0.15)
        // It was the ceiling that ended it, not the countdown: the dwell never ran.
        #expect(countdown.remaining > 0)
    }

    /// A real drag still gets its pause — the toast is not pulled out from under a moving thumb.
    @Test func aBriefHoldPostponesTheDismissal() {
        let dwell: TimeInterval = 3
        var held = MonacoToastCountdown(dwell: dwell)
        // Held for the first second, then released.
        let result = run(&held, isHeld: { $0 < 10 })

        #expect(result.finished)
        #expect(result.elapsed > dwell, "the hold bought no extra time at all")
        #expect(abs(result.elapsed - (dwell + 1)) < 0.15)
    }

    /// The ceiling is wall clock from the toast appearing, so repeated holds cannot walk it out.
    @Test func theCeilingIsNotResetByLettingGoAndGrabbingAgain() {
        let dwell: TimeInterval = 3
        var countdown = MonacoToastCountdown(dwell: dwell)
        // Held on every other tick, forever.
        let result = run(&countdown, isHeld: { $0.isMultiple(of: 2) })

        #expect(result.finished)
        #expect(result.elapsed <= dwell + MonacoToastTiming.maximumHold + 0.15)
    }
}

struct MonacoToastPlacementTests {
    /// One line of `.body` at the default text size.
    private let defaultLine = MonacoToastPlacement.bottomCTALabelLineHeight
    /// One line of `.body` at AX5: 17pt type becomes 53pt, at roughly 1.24 line height.
    private let accessibilityLine: CGFloat = 65.5

    private var bar: CGFloat { MonacoToastPlacement.bottomCTAButtonHeight + MonacoToastPlacement.bottomCTAChrome }

    /// The inset is sized against the button's own floor, not a second copy of the number.
    @Test func theBarHeightComesFromTheButtonItself() {
        #expect(MonacoToastPlacement.bottomCTAButtonHeight == MonacoButtonMetrics.minimumHeight)
    }

    @Test func screenBottomIgnoresTheBottomBar() {
        #expect(MonacoToastPlacement.screenBottom.bottomInset(scaledLabelLineHeight: defaultLine)
            == MonacoToastPlacement.gap)
        #expect(MonacoToastPlacement.screenBottom.bottomInset(scaledLabelLineHeight: accessibilityLine)
            == MonacoToastPlacement.gap)
    }

    @Test func aboveBottomCTAClearsTheBarAtTheDefaultTextSize() {
        let inset = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: defaultLine)
        #expect(inset == bar + MonacoToastPlacement.gap)
        #expect(inset >= 72)
    }

    /// The button is `max(floor, one line)`, so a bar does not grow at all until the line
    /// outgrows the floor. The old model scaled the floor itself and started growing immediately.
    @Test func theBarDoesNotGrowUntilTheLabelOutgrowsTheButtonFloor() {
        let atDefault = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: defaultLine)
        let atLarge = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: 40)
        #expect(atLarge == atDefault)
    }

    @Test func aboveBottomCTAGrowsWithTheLabelOnceItPassesTheFloor() {
        let inset = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: accessibilityLine)
        let expectedBar = accessibilityLine + MonacoToastPlacement.bottomCTAChrome
        #expect(inset == expectedBar + MonacoToastPlacement.gap)
    }

    /// The overshoot this replaced: `@ScaledMetric` on the 50pt floor reached ~156pt at AX5, for a
    /// bottom inset of ~188pt over a bar that is really about 86pt — the toast floated ~100pt
    /// above the CTA. The bar grows by its label's line height, not in proportion to the floor.
    @Test func theInsetDoesNotFloatAboveTheBarAtAccessibilitySizes() {
        let inset = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: accessibilityLine)
        let realBar = accessibilityLine + MonacoToastPlacement.bottomCTAChrome
        #expect(inset - realBar == MonacoToastPlacement.gap)
        #expect(inset < 110, "the toast is floating above the bar again")
    }

    /// Half of the overshoot was the *base* the modifier scaled, not the formula: `@ScaledMetric`
    /// on the 50pt button floor gives ~156pt at AX5, for an inset of ~188pt over a bar that is
    /// really ~86pt. A floor is a minimum to compare against, not a quantity to multiply. The base
    /// is now a line of body text, which is what actually grows when the bar grows.
    @Test func theScaledBaseIsALineOfTextNotTheButtonFloor() {
        #expect(MonacoToastPlacement.bottomCTALabelLineHeight < MonacoToastPlacement.bottomCTAButtonHeight)

        // Body type roughly triples between the default size and AX5 (17pt -> 53pt).
        let ax5Factor: CGFloat = 3.12
        let scaledLine = MonacoToastPlacement.bottomCTALabelLineHeight * ax5Factor
        let scaledFloor = MonacoToastPlacement.bottomCTAButtonHeight * ax5Factor

        #expect(MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: scaledLine) < 110)
        // What the old base produced, kept here so the two models can be compared at a glance.
        #expect(MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledLabelLineHeight: scaledFloor) > 170)
    }

    @Test func aCallerInsetSizedForABarAlsoGrows() {
        // The money screens pass 72, which was "12 + 50 + 8, plus a gap" at the default size.
        let atDefault = MonacoToastPlacement.custom(72).bottomInset(scaledLabelLineHeight: defaultLine)
        let scaled = MonacoToastPlacement.custom(72).bottomInset(scaledLabelLineHeight: accessibilityLine)
        let growth = accessibilityLine - MonacoToastPlacement.bottomCTAButtonHeight
        #expect(atDefault == 72)
        #expect(scaled == 72 + growth)
    }

    @Test func aSmallCallerInsetIsLeftAlone() {
        #expect(MonacoToastPlacement.custom(12).bottomInset(scaledLabelLineHeight: accessibilityLine) == 12)
    }

    /// The `.custom` threshold is a heuristic on the magnitude of the caller's number, so it
    /// cannot tell a taller bar from a shorter one. Documented here so #398 has the case written
    /// down rather than discovered.
    @Test func aCallerInsetForATallerBarGetsTheSameGrowthAsAShortOne() {
        let short = MonacoToastPlacement.custom(72).bottomInset(scaledLabelLineHeight: accessibilityLine) - 72
        let tall = MonacoToastPlacement.custom(108).bottomInset(scaledLabelLineHeight: accessibilityLine) - 108
        #expect(short == tall)
    }
}
