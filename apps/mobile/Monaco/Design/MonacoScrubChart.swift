import Charts
import MonacoCore
import SwiftUI

/// The one price curve in the app: draw-on, scrub, dashed baseline, live end dot.
///
/// It is deliberately a design-system view rather than a piece of the stock screen.
/// Home's P&L strip and the cabals' P&L strip draw the same shape from different
/// data, and three hand-rolled `Chart`s are three chances for the interaction to
/// drift — a scrub on one screen and none on the next reads as a bug in the app,
/// not as a difference between screens.
///
/// What it does not do: decide anything. The tint, the baseline, the summary, the
/// value under the finger and the nearest-sample search are all passed in, because
/// the screen owns the meaning of its own numbers — `AssetChartSeries` is where the
/// stock screen's meaning lives.
struct MonacoScrubChart: View {
    struct Point: Equatable {
        let date: Date
        let value: Double

        init(date: Date, value: Double) {
            self.date = date
            self.value = value
        }
    }

    /// Ascending by date.
    let points: [Point]
    /// Gain/loss colour for the line and the area under it.
    var tint: Color
    /// Where the dashed rule goes — the previous session's close. Nil draws no rule.
    var baseline: Double?
    var height: CGFloat = 200
    /// Pulses the dot at the end of the line. True when the price behind the curve is
    /// still moving: the market is open, or the token trades around the clock.
    var isLive: Bool = false
    /// Changing this replays the draw-on. The screen passes what identifies the
    /// *window* on screen — not the shape of its data, or a quiet re-read that
    /// returns one more bar would wipe the curve under the member's finger.
    var drawOnKey: AnyHashable
    /// The sample under the finger, as an index into `points`. Nil when nobody is
    /// scrubbing, which is also what a release restores.
    @Binding var selection: Int?
    /// One sentence describing the whole curve, for VoiceOver.
    var summary: String
    /// Which sample a touch at this instant lands on. Owned by the screen so the
    /// drag and the tests measure with one implementation: `AssetChartSeries` has
    /// the search, and a copy here would be a second one free to drift from it.
    var nearestIndex: (Date) -> Int?
    /// What VoiceOver reads for one sample — "$231.40, Tue 2:05 PM".
    var describePoint: (Int) -> String
    var accessibilityIdentifier: String = "monaco-scrub-chart"

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scrubSelectionPersists) private var selectionPersists
    @State private var drawProgress: CGFloat = 0
    @State private var areaOpacity: Double = 0
    @State private var dotOpacity: Double = 0
    /// Whether this view has ever played its draw-on. It is per-view state, so it
    /// survives a push and a pop: coming back from a cancelled propose flow must not
    /// redraw a chart that is already on screen.
    @State private var hasDrawn = false
    @State private var dragIntent: DragIntent = .undecided

    private var selectedPoint: Point? {
        guard let selection, points.indices.contains(selection) else { return nil }
        return points[selection]
    }

    /// Prices live far from zero, so the plot is clipped to the series' own range —
    /// plus the baseline, which would otherwise sit outside the chart on a day that
    /// gapped away from the previous close and never come back.
    ///
    /// Read once per body, never per mark: it walks the whole series, and the body
    /// re-evaluates on every frame of a drag.
    private var valueDomain: ClosedRange<Double> {
        var low = baseline ?? points.first?.value ?? 0
        var high = low
        for point in points {
            low = Swift.min(low, point.value)
            high = Swift.max(high, point.value)
        }
        let pad = Swift.max((high - low) * 0.12, 0.01)
        return (low - pad)...(high + pad)
    }

    var body: some View {
        chart
            .frame(height: height)
            .onChange(of: drawOnKey) { _, _ in replayDrawOn() }
            .onAppear { if !hasDrawn { replayDrawOn() } }
            // A tick per sample the finger crosses, which is what makes a drag feel
            // like it is touching the data rather than sliding over a picture. It is
            // the one place in the app that reaches past the `Haptics` vocabulary:
            // that vocabulary is a call, and a call per sample during a drag means
            // priming a generator by hand on every frame. Nothing on release — the
            // selection clearing is the end of the gesture, not a sample crossed.
            .sensoryFeedback(trigger: selection) { _, new in
                new == nil ? nil : .selection
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(summary)
            .accessibilityValue(voiceOverValue)
            .accessibilityAdjustableAction(step)
            // VoiceOver's own way out of the scrub: a three-finger swipe puts the
            // curve back on its last sample, the same as lifting a finger does.
            .accessibilityScrollAction { _ in selection = nil }
            .accessibilityIdentifier(accessibilityIdentifier)
    }

    private var chart: some View {
        // Hoisted: every mark below would otherwise walk the series again, and a
        // 250-bar year chart re-evaluates this body on every frame of a drag.
        let domain = valueDomain
        return Chart {
            if let baseline {
                RuleMark(y: .value("Previous close", baseline))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 5]))
                    .foregroundStyle(MonacoTheme.muted.opacity(0.55))
            }
            // By index, not by date: two samples at one instant would be two marks
            // with one identity. `AssetChartSeries` already collapses repeats, and
            // this keeps the component honest for any other series passed to it.
            ForEach(points.indices, id: \.self) { index in
                AreaMark(
                    x: .value("Time", points[index].date),
                    yStart: .value("Floor", domain.lowerBound),
                    yEnd: .value("Price", points[index].value)
                )
                .foregroundStyle(areaGradient)
                .opacity(areaOpacity)
                // Monotone, not Catmull-Rom: a sparse series must not draw peaks the
                // data never had.
                .interpolationMethod(.monotone)

                LineMark(
                    x: .value("Time", points[index].date),
                    y: .value("Price", points[index].value)
                )
                .foregroundStyle(tint)
                .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                .interpolationMethod(.monotone)
            }
            if let selectedPoint {
                RuleMark(x: .value("Time", selectedPoint.date))
                    .lineStyle(StrokeStyle(lineWidth: 1))
                    .foregroundStyle(MonacoTheme.ink.opacity(0.35))
                PointMark(
                    x: .value("Time", selectedPoint.date),
                    y: .value("Price", selectedPoint.value)
                )
                .symbolSize(120)
                .foregroundStyle(tint)
            }
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: domain)
        // The plot stops short of the trailing edge so the dot at the end of the line
        // has room to be a circle: at the frame's edge the mask cut it in half, which
        // read as a rendering glitch rather than as "the price is here".
        .chartPlotStyle { $0.background(Color.clear).padding(.trailing, 12) }
        .chartOverlay { proxy in
            GeometryReader { geometry in
                ZStack {
                    liveDot(proxy, in: geometry)
                    // The drag is owned here rather than left to `chartXSelection`,
                    // for two reasons: a selection has to be dropped the moment the
                    // finger leaves — a broker's chart snaps back — and the gesture
                    // has to sit where a `ChartProxy` can turn an x into a date.
                    Rectangle()
                        .fill(.clear)
                        .contentShape(Rectangle())
                        .simultaneousGesture(scrubGesture(proxy, in: geometry))
                }
            }
        }
        .mask(alignment: .leading) { drawOnMask }
    }

    // MARK: - Scrubbing with a finger

    /// Which gesture a touch on the plot turned out to be.
    ///
    /// The chart sits in the middle of a `ScrollView`, so the plot is also the most
    /// natural place on the screen to put a thumb and push the cards below into
    /// view. A zero-distance drag claims that touch on touch-down and makes the
    /// chart a dead zone for scrolling; the first few points of movement are what
    /// say which gesture the member meant.
    private enum DragIntent {
        case undecided
        case scrubbing
        /// Vertical: the scroll view's, and this gesture keeps its hands off it for
        /// the rest of the drag.
        case scrolling
    }

    /// The overlay spans the whole chart while the proxy measures from the plot
    /// area's own origin, so the touch is moved into the plot's coordinates before
    /// it is turned into a date. Skipping that subtraction reads the price a few
    /// samples to the left of the finger.
    private func scrubGesture(_ proxy: ChartProxy, in geometry: GeometryProxy) -> some Gesture {
        // Enough travel to have a direction, short enough that the dot still feels
        // like it is under the finger from the first moment it appears.
        DragGesture(minimumDistance: 10)
            .onChanged { value in
                if dragIntent == .undecided {
                    dragIntent = abs(value.translation.height) > abs(value.translation.width)
                        ? .scrolling
                        : .scrubbing
                }
                guard dragIntent == .scrubbing else { return }
                guard let plotAnchor = proxy.plotFrame else { return }
                let x = value.location.x - geometry[plotAnchor].minX
                guard let date: Date = proxy.value(atX: x) else { return }
                selection = nearestIndex(date)
            }
            .onEnded { _ in
                dragIntent = .undecided
                if !selectionPersists { selection = nil }
            }
    }

    private var areaGradient: LinearGradient {
        LinearGradient(
            colors: [tint.opacity(0.26), tint.opacity(0)],
            startPoint: .top,
            endPoint: .bottom
        )
    }

    // MARK: - Motion

    /// The curve is revealed left to right. A mask rather than a `trim`, because the
    /// area fill under the line has to be revealed with it — trimming a filled shape
    /// leaves the fill behind and the line drawing over nothing.
    private var drawOnMask: some View {
        GeometryReader { geometry in
            Rectangle()
                .frame(width: geometry.size.width * drawProgress)
        }
    }

    private func replayDrawOn() {
        hasDrawn = true
        // Never under a finger: the mask does not survive the wipe even though the
        // selection does, so the sample being read would vanish mid-read.
        guard !reduceMotion, selection == nil else {
            drawProgress = 1
            areaOpacity = 1
            dotOpacity = 1
            return
        }
        drawProgress = 0
        areaOpacity = 0
        dotOpacity = 0
        withAnimation(.easeOut(duration: 0.45)) { drawProgress = 1 }
        // The wash follows the line rather than racing it.
        withAnimation(.easeOut(duration: 0.4).delay(0.1)) { areaOpacity = 1 }
        // And the dot arrives with the end of the line. It cannot be read off
        // `drawProgress`: that is set to its target synchronously inside
        // `withAnimation`, so a body reading it sees 1 immediately and the dot pops
        // in at the *start* of the wipe.
        withAnimation(.easeOut(duration: 0.15).delay(0.45)) { dotOpacity = 1 }
    }

    /// The dot at the end of the line, with a slow halo while the price behind it is
    /// still moving. Static under Reduce Motion, and absent when nothing is live —
    /// the pulse is a claim that the number is current, so it must not be decoration.
    @ViewBuilder
    private func liveDot(_ proxy: ChartProxy, in geometry: GeometryProxy) -> some View {
        if let last = points.last,
           let plotAnchor = proxy.plotFrame,
           let x = proxy.position(forX: last.date),
           let y = proxy.position(forY: last.value) {
            let plot = geometry[plotAnchor]
            ZStack {
                Circle()
                    .fill(tint.opacity(0.28))
                    .frame(width: 20, height: 20)
                    .opacityLoop(to: 0.15, halfPeriod: 1.6, active: isLive && !reduceMotion)
                    .opacity(isLive ? 1 : 0)
                Circle()
                    .fill(tint)
                    .frame(width: 7, height: 7)
            }
            .position(x: plot.minX + x, y: plot.minY + y)
            .opacity(dotOpacity)
            .allowsHitTesting(false)
        }
    }

    // MARK: - VoiceOver

    /// Without an adjustable action a scrubbing chart is unusable with VoiceOver on:
    /// the drag never reaches the chart. Swiping up and down walks the samples and
    /// reads each one, which is the same interaction the finger gets — including the
    /// way out of it, because an adjustable action has no release to clear on.
    private func step(_ direction: AccessibilityAdjustmentDirection) {
        selection = MonacoScrubStep.next(
            from: selection,
            forward: direction == .increment,
            count: points.count
        )
    }

    private var voiceOverValue: String {
        guard !points.isEmpty else { return "No price history" }
        return describePoint(selection ?? points.count - 1)
    }
}

/// Where a VoiceOver increment or decrement leaves the scrub.
///
/// Nil is not "nothing selected" so much as "one step past the end": the live price,
/// which is what the screen shows when no finger is down. Walking forward off the
/// last sample therefore lands back on it, and that is the only way out — an
/// adjustable action has no release, so a scrub that could not be walked off the end
/// would pin the hero to a historical sample for as long as the screen was open.
enum MonacoScrubStep {
    static func next(from selection: Int?, forward: Bool, count: Int) -> Int? {
        guard count > 0 else { return nil }
        if forward {
            guard let selection else { return nil }
            let next = selection + 1
            return next < count ? next : nil
        }
        // Nil sits one past the last sample, so stepping back from it selects that
        // sample rather than skipping it.
        let current = selection ?? count
        return Swift.max(Swift.min(current, count) - 1, 0)
    }
}

// MARK: - Holding a scrub open, for tests

private struct ScrubSelectionPersistsKey: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    /// Keeps the scrubbed sample selected after the finger lifts.
    ///
    /// False everywhere the app ships; the debug sample harness is the only thing
    /// that sets it, and only when launched with `-MonacoScrubHolds`. It exists
    /// because XCUITest's gesture calls return *after* the lift — a press-drag-hold
    /// is one synthesised gesture, so by the time a test can read the screen the
    /// chart has already snapped back and "does the hero follow the finger" is
    /// unaskable. With this on, a test can drag and then look.
    var scrubSelectionPersists: Bool {
        get { self[ScrubSelectionPersistsKey.self] }
        set { self[ScrubSelectionPersistsKey.self] = newValue }
    }
}
