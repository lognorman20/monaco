import MonacoCore
import SwiftUI
import Testing
@testable import Monaco

/// The parts of the scrubbing chart that are decisions rather than drawing: where a
/// VoiceOver step lands, what makes the curve replay its draw-on, and whether the
/// price-tick flash has anything to animate at all.
///
/// All three were review findings that a simulator test cannot see — a wipe, a
/// wash and an adjustable action leave no trace in the accessibility tree — so they
/// are pinned here, on the host, against the same values the views play.
struct MonacoScrubStepTests {
    /// The bug: `step()` clamped an increment to the last sample, and nothing else
    /// ever cleared the selection. One swipe up with VoiceOver on pinned the hero to
    /// a historical sample for the life of the screen — no live price, no tick, and
    /// a change row labelled with a past time.
    @Test func steppingForwardOffTheLastSampleGoesBackToLive() {
        #expect(MonacoScrubStep.next(from: 2, forward: true, count: 4) == 3)
        #expect(MonacoScrubStep.next(from: 3, forward: true, count: 4) == nil)
        // And it stays there rather than wrapping round to the start.
        #expect(MonacoScrubStep.next(from: nil, forward: true, count: 4) == nil)
    }

    /// Stepping back from live selects the last sample, not the one before it.
    @Test func steppingBackFromLiveTakesTheLastSample() {
        #expect(MonacoScrubStep.next(from: nil, forward: false, count: 4) == 3)
        #expect(MonacoScrubStep.next(from: 3, forward: false, count: 4) == 2)
    }

    @Test func steppingBackOffTheStartStaysOnTheFirstSample() {
        #expect(MonacoScrubStep.next(from: 0, forward: false, count: 4) == 0)
    }

    /// A shorter series arriving under a finger must not leave the step arithmetic
    /// pointing past the end of it.
    @Test func aSelectionPastTheEndIsBroughtBackInsideTheSeries() {
        #expect(MonacoScrubStep.next(from: 99, forward: false, count: 4) == 3)
        #expect(MonacoScrubStep.next(from: 99, forward: true, count: 4) == nil)
    }

    @Test func anEmptyCurveHasNothingToSelect() {
        #expect(MonacoScrubStep.next(from: nil, forward: false, count: 0) == nil)
        #expect(MonacoScrubStep.next(from: 0, forward: true, count: 0) == nil)
    }
}

struct MonacoPriceTickFlashTests {
    private var timeline: KeyframeTimeline<Double> {
        KeyframeTimeline(initialValue: 0.0) { MonacoPriceTickFlash.keyframes(0.0) }
    }

    /// The bug: the flash was two writes to one `@State` inside a single synchronous
    /// block — `intensity = 0.35` and then `withAnimation { intensity = 0 }`. SwiftUI
    /// commits one transaction per update, so the net change was 0 → 0: nothing to
    /// interpolate, and the wash never rendered. A keyframe track is a function of
    /// time, so it cannot be cancelled out this way — and it can be read here.
    @Test func theWashActuallyLeavesZero() {
        let track = timeline
        #expect(track.value(time: 0) == 0)
        #expect(track.value(time: MonacoPriceTickFlash.rise) > 0.3)
        // Visible for long enough to be seen, and gone by the end.
        #expect(track.value(time: MonacoPriceTickFlash.rise + 0.1) > 0)
        #expect(track.duration > 0.25)
    }

    /// It has to land back at nothing, or a pill that once ticked would stay tinted.
    @Test func theWashFadesAllTheWayBack() {
        let track = timeline
        #expect(abs(track.value(time: track.duration)) < 0.001)
    }

    /// A wash, not the full colour: at full strength the label inside the pill stops
    /// clearing contrast for the 250ms it matters most. This is not a formality — a
    /// `CubicKeyframe` fall infers its tangents from its neighbours and overshot the
    /// peak by half again, which this caught.
    @Test func theWashNeverGoesDarkerThanItsPeak() {
        let track = timeline
        for step in 0...50 {
            let time = track.duration * Double(step) / 50
            let intensity = track.value(time: time)
            #expect(intensity <= MonacoPriceTickFlash.peak + 0.001)
            #expect(intensity >= 0)
        }
    }
}

@MainActor
struct AssetChartDrawOnKeyTests {
    private func series(_ range: AssetChartRange, bars: Int, from start: Int64 = 1_000) -> AssetChartSeries {
        AssetChartSeries(
            range: range,
            points: (0..<bars).map {
                AssetChartPointDTO(
                    timestamp: start + Int64($0) * 300,
                    priceUsdcMicros: 100_000_000 + Int64($0) * 10_000
                )
            }
        )
    }

    /// The bug: the key included the series' shape — its sample count and its first
    /// and last timestamps — so the two-minute background re-read of a live day chart
    /// changed it on essentially every tick and the curve wiped itself left to right,
    /// unasked, including under a member's finger. The draw-on belongs to the window,
    /// not to the data inside it.
    @Test func aQuietReReadThatGrowsTheWindowDoesNotReplayTheDrawOn() {
        let drawn = AssetChartCard.drawOnKey(series(.oneDay, bars: 78))
        let oneBarLater = AssetChartCard.drawOnKey(series(.oneDay, bars: 79, from: 1_300))

        #expect(drawn == oneBarLater)
    }

    /// A different window is a different curve, and that is what the wipe is for.
    @Test func aDifferentRangeReplaysTheDrawOn() {
        #expect(AssetChartCard.drawOnKey(series(.oneDay, bars: 78)) != AssetChartCard.drawOnKey(series(.oneYear, bars: 78)))
    }
}
