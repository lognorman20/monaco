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
/// What it does not do: decide anything. The tint, the baseline, the summary and
/// the value under the finger are all passed in, because the screen owns the
/// meaning of its own numbers — `AssetChartSeries` is where the stock screen's
/// meaning lives.
struct MonacoScrubChart: View {
    struct Point: Identifiable, Equatable {
        let date: Date
        let value: Double

        var id: Date { date }

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
    /// Changing this replays the draw-on. The screen passes whatever identifies the
    /// series it just loaded — the range, plus enough of the data to notice a reload.
    var drawOnKey: AnyHashable
    /// The sample under the finger, as an index into `points`. Nil when nobody is
    /// scrubbing, which is also what a release restores.
    @Binding var selection: Int?
    /// One sentence describing the whole curve, for VoiceOver.
    var summary: String
    /// What VoiceOver reads for one sample — "$231.40, Tue 2:05 PM".
    var describePoint: (Int) -> String
    var accessibilityIdentifier: String = "monaco-scrub-chart"

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var scrubDate: Date?
    @State private var drawProgress: CGFloat = 0
    @State private var areaOpacity: Double = 0

    private var selectedPoint: Point? {
        guard let selection, points.indices.contains(selection) else { return nil }
        return points[selection]
    }

    /// Prices live far from zero, so the plot is clipped to the series' own range —
    /// plus the baseline, which would otherwise sit outside the chart on a day that
    /// gapped away from the previous close and never come back.
    private var valueDomain: ClosedRange<Double> {
        let values = points.map(\.value) + [baseline].compactMap { $0 }
        let low = values.min() ?? 0
        let high = values.max() ?? 1
        let pad = max((high - low) * 0.12, 0.01)
        return (low - pad)...(high + pad)
    }

    var body: some View {
        chart
            .frame(height: height)
            .chartXSelection(value: $scrubDate)
            // `chartXSelection` alone leaves a tapped selection standing. A broker's
            // chart snaps back the moment the finger leaves it, so the gesture is
            // owned here and cleared on `onEnded`.
            .chartGesture { proxy in
                DragGesture(minimumDistance: 0)
                    .onChanged { proxy.selectXValue(at: $0.location.x) }
                    .onEnded { _ in scrubDate = nil }
            }
            .onChange(of: scrubDate) { _, date in
                selection = date.flatMap(nearestIndex(to:))
            }
            .onChange(of: drawOnKey) { _, _ in replayDrawOn() }
            .onAppear { replayDrawOn() }
            // A tick per sample the finger crosses, which is what makes a drag feel
            // like it is touching the data rather than sliding over a picture.
            .sensoryFeedback(.selection, trigger: selection)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(summary)
            .accessibilityValue(voiceOverValue)
            .accessibilityAdjustableAction(step)
            .accessibilityIdentifier(accessibilityIdentifier)
    }

    private var chart: some View {
        Chart {
            if let baseline {
                RuleMark(y: .value("Previous close", baseline))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 5]))
                    .foregroundStyle(MonacoTheme.muted.opacity(0.55))
            }
            ForEach(points) { point in
                AreaMark(
                    x: .value("Time", point.date),
                    yStart: .value("Floor", valueDomain.lowerBound),
                    yEnd: .value("Price", point.value)
                )
                .foregroundStyle(areaGradient)
                .opacity(areaOpacity)
                // Monotone, not Catmull-Rom: a sparse series must not draw peaks the
                // data never had.
                .interpolationMethod(.monotone)

                LineMark(
                    x: .value("Time", point.date),
                    y: .value("Price", point.value)
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
        .chartYScale(domain: valueDomain)
        .chartPlotStyle { $0.background(Color.clear) }
        .chartOverlay { proxy in
            liveDot(proxy)
        }
        .mask(alignment: .leading) { drawOnMask }
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
        guard !reduceMotion else {
            drawProgress = 1
            areaOpacity = 1
            return
        }
        drawProgress = 0
        areaOpacity = 0
        withAnimation(.easeOut(duration: 0.45)) { drawProgress = 1 }
        // The wash follows the line rather than racing it.
        withAnimation(.easeOut(duration: 0.4).delay(0.1)) { areaOpacity = 1 }
    }

    /// The dot at the end of the line, with a slow halo while the price behind it is
    /// still moving. Static under Reduce Motion, and absent when nothing is live —
    /// the pulse is a claim that the number is current, so it must not be decoration.
    @ViewBuilder
    private func liveDot(_ proxy: ChartProxy) -> some View {
        GeometryReader { geometry in
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
                .opacity(drawProgress >= 1 ? 1 : 0)
            }
        }
        .allowsHitTesting(false)
    }

    // MARK: - Selection

    private func nearestIndex(to date: Date) -> Int? {
        guard !points.isEmpty else { return nil }
        var low = 0
        var high = points.count - 1
        if date <= points[low].date { return low }
        if date >= points[high].date { return high }
        while low + 1 < high {
            let mid = (low + high) / 2
            if points[mid].date == date { return mid }
            if points[mid].date < date {
                low = mid
            } else {
                high = mid
            }
        }
        let before = date.timeIntervalSince(points[low].date)
        let after = points[high].date.timeIntervalSince(date)
        return after < before ? high : low
    }

    // MARK: - VoiceOver

    /// Without an adjustable action a scrubbing chart is unusable with VoiceOver on:
    /// the drag never reaches the chart. Swiping up and down walks the samples and
    /// reads each one, which is the same interaction the finger gets.
    private func step(_ direction: AccessibilityAdjustmentDirection) {
        guard !points.isEmpty else { return }
        let current = selection ?? points.count - 1
        switch direction {
        case .increment:
            selection = min(current + 1, points.count - 1)
        case .decrement:
            selection = max(current - 1, 0)
        @unknown default:
            break
        }
    }

    private var voiceOverValue: String {
        guard !points.isEmpty else { return "No price history" }
        return describePoint(selection ?? points.count - 1)
    }
}
