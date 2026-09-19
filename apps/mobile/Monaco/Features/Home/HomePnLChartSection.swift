import Charts
import SwiftUI

/// A slim, chrome-free P&L strip — no title, no axes. Real cabals have only a
/// few NAV snapshots, so this (and the caller) hide the whole section under 3 points
/// rather than show a scrub-able chart that would read as broken.
///
/// `onInk` is the Home hero variant: it sits inside the deep ink money card, so it uses the
/// saturated P&L pair and a heavier gradient area fill that reads against ink.
struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]
    var onInk = false
    var height: CGFloat = 120

    private var isUp: Bool { (points.last?.chartValue ?? 0) >= 0 }

    private var chartTint: Color {
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

    var body: some View {
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
        .accessibilityIdentifier("home-pnl-chart")
    }
}
