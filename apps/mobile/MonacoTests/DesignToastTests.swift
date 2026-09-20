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
}

struct MonacoToastPlacementTests {
    private let defaultButton = MonacoToastPlacement.bottomCTAButtonHeight
    /// `BottomCTA`'s 50pt button floor at AX5.
    private let scaledButton: CGFloat = 88

    @Test func screenBottomIgnoresTheBottomBar() {
        #expect(MonacoToastPlacement.screenBottom.bottomInset(scaledButtonHeight: defaultButton)
            == MonacoToastPlacement.gap)
        #expect(MonacoToastPlacement.screenBottom.bottomInset(scaledButtonHeight: scaledButton)
            == MonacoToastPlacement.gap)
    }

    @Test func aboveBottomCTAClearsTheBarAtTheDefaultTextSize() {
        let inset = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledButtonHeight: defaultButton)
        #expect(inset == defaultButton + MonacoToastPlacement.bottomCTAChrome + MonacoToastPlacement.gap)
        #expect(inset >= 72)
    }

    @Test func aboveBottomCTAGrowsWithTheButton() {
        let base = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledButtonHeight: defaultButton)
        let scaled = MonacoToastPlacement.aboveBottomCTA.bottomInset(scaledButtonHeight: scaledButton)
        #expect(scaled - base == scaledButton - defaultButton)
    }

    @Test func aCallerInsetSizedForABarAlsoGrows() {
        // The four money screens pass 72, which was "12 + 50 + 8, plus a gap" at the default size.
        let atDefault = MonacoToastPlacement.custom(72).bottomInset(scaledButtonHeight: defaultButton)
        let scaled = MonacoToastPlacement.custom(72).bottomInset(scaledButtonHeight: scaledButton)
        #expect(atDefault == 72)
        #expect(scaled == 72 + (scaledButton - defaultButton))
    }

    @Test func aSmallCallerInsetIsLeftAlone() {
        #expect(MonacoToastPlacement.custom(12).bottomInset(scaledButtonHeight: scaledButton) == 12)
    }
}
