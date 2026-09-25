import Charts
import SwiftUI

/// A slim, chrome-free P&L strip — no title, no axes. Real cabals have only a
/// few NAV snapshots, so this (and the caller) hide the whole section under 3 points
/// rather than show a scrub-able chart that would read as broken.
///
/// `onInk` is for a deep ink surface: the saturated P&L pair and a heavier area fill
/// that reads against ink. On the paper it draws the plain pair with a light wash.
struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]
    var onInk = false
    var height: CGFloat = 120

    /// The window's own direction — where the curve ends against where it starts — not the
    /// lifetime sign. A portfolio down over the hour but up all time drew a green falling
    /// line before (#327).
    private var windowChange: Double {
        guard let first = points.first?.chartValue, let last = points.last?.chartValue else { return 0 }
        return last - first
    }

    private var isUp: Bool { windowChange >= 0 }

    /// What the line says, in one sentence, for VoiceOver — naming the same window the caption
    /// under the curve names, so the two do not disagree.
    private var accessibilitySummary: String {
        PnLSpeech.dollars(String(format: "%+.2f", windowChange)) + " over the past hour"
    }

    private var chartTint: Color {
        if onInk {
            return isUp ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
        }
        return isUp ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
    }

    /// Gradient area fill tinted by gain/loss: strongest under the line, clear at the baseline.
    private var areaGradient: LinearGradient {
        LinearGradient(
            colors: [chartTint.opacity(onInk ? 0.38 : 0.18), chartTint.opacity(0)],
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
        }
    }

    private var chart: some View {
        Chart(points) { point in
            AreaMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(areaGradient)
            .interpolationMethod(.monotone)
            LineMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(chartTint)
            .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
            .interpolationMethod(.monotone)
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
