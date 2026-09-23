import Charts
import SwiftUI

/// A slim, chrome-free P&L strip — no title, no axes. Real cabals have only a
/// few NAV snapshots, so this (and the caller) hide the whole section under 3 points
/// rather than show a scrub-able chart that would read as broken.
///
/// `onInk` is the ink-fold variant: it sits on the slab that opens Home, so it uses the
/// saturated P&L pair and a heavier gradient area fill that reads against ink.
///
/// The curve draws on **only when the range changes** (§4 #6). A poll landing three more points
/// must not re-sweep a curve the member is already reading, and under Reduce Motion the mask is
/// not applied at all — the curve is simply drawn complete.
struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]
    var onInk = false
    var height: CGFloat = 120
    /// Overrides the gain/loss colour. Used where the curve belongs to something with an
    /// identity of its own — a cabal's tint on Profile — rather than to a direction.
    var tint: Color?
    /// The window the curve describes, when the caller wants this component to say so. Purely a
    /// redraw trigger here: the caller owns the fetch.
    var range: HomeLeaderboardRange?

    /// The draw-on sweep (§4 #6). Longer than any of `MonacoMotion`'s four curves on purpose —
    /// this is a curve being drawn, not a control responding — and it goes through `.reduced(_:)`
    /// like every other animation in the app, so the Reduce Motion gate is visible at the call
    /// site rather than hidden in a `guard`.
    private static let drawOn = Animation.easeOut(duration: 0.55)

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// 0 → 1 across the plot width. Re-run on a range change, never on a data poll.
    @State private var sweep: CGFloat = 0

    init(
        points: [HomePnLSeriesPointDTO],
        onInk: Bool = false,
        height: CGFloat = 120,
        tint: Color? = nil,
        range: HomeLeaderboardRange? = nil
    ) {
        self.points = points
        self.onInk = onInk
        self.height = height
        self.tint = tint
        self.range = range
    }

    /// Cross-chunk contract 2 — Chunk F reuses the Home curve on Profile.
    ///
    /// The binding is the caller's: Profile owns which window it is asking the API for, exactly
    /// as Home does. This initialiser exists so the two cannot drift into two curves.
    init(points: [HomePnLSeriesPointDTO], range: Binding<HomeLeaderboardRange>, tint: Color) {
        self.init(points: points, onInk: false, height: 120, tint: tint, range: range.wrappedValue)
    }

    /// The window's own direction — where the curve ends against where it starts — not the
    /// lifetime sign. A portfolio down over the hour but up all time drew a green falling
    /// line before (#327).
    private var windowChange: Double {
        guard let first = points.first?.chartValue, let last = points.last?.chartValue else { return 0 }
        return last - first
    }

    private var isUp: Bool { windowChange >= 0 }

    /// What the line says, in one sentence, for VoiceOver — naming the same window the caption
    /// above the curve names, so the two do not disagree.
    private var accessibilitySummary: String {
        let phrase = range?.windowPhrase ?? "the past hour"
        return PnLSpeech.dollars(String(format: "%+.2f", windowChange)) + " over \(phrase)"
    }

    private var chartTint: Color {
        if let tint { return tint }
        if onInk {
            return isUp ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
        }
        return isUp ? MonacoTheme.profit : MonacoTheme.loss
    }

    /// Gradient area fill tinted by gain/loss: strongest under the line, clear at the baseline.
    private var areaGradient: LinearGradient {
        LinearGradient(
            colors: [chartTint.opacity(onInk ? 0.38 : 0.22), chartTint.opacity(0)],
            startPoint: .top,
            endPoint: .bottom
        )
    }

    /// A flat two-point line reads as broken, so the strip draws nothing under three points
    /// rather than rendering a chart VoiceOver would have to describe as "no data".
    @ViewBuilder
    var body: some View {
        if points.count >= 3 {
            chart
                .mask(alignment: .leading) { sweepMask }
                .onChange(of: range) { _, _ in
                    sweep = 0
                    withAnimation(Self.drawOn.reduced(reduceMotion)) { sweep = 1 }
                }
                .onAppear {
                    withAnimation(Self.drawOn.reduced(reduceMotion)) { sweep = 1 }
                }
        }
    }

    /// Left-to-right reveal. Under Reduce Motion — and any time `sweep` has already finished —
    /// this is a full-width rectangle, which costs nothing.
    private var sweepMask: some View {
        GeometryReader { proxy in
            Rectangle()
                .frame(width: reduceMotion ? proxy.size.width : proxy.size.width * sweep)
        }
    }

    private var chart: some View {
        Chart(points) { point in
            AreaMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(areaGradient)
            .interpolationMethod(.catmullRom)
            LineMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(chartTint)
            .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
            .interpolationMethod(.catmullRom)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartPlotStyle { $0.background(Color.clear) }
        .frame(height: height)
        // Without this VoiceOver reads every "Time / P&L" mark in turn. One sentence says
        // what the curve says.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("P&L curve")
        .accessibilityValue(accessibilitySummary)
        .accessibilityIdentifier("home-pnl-chart")
    }
}
