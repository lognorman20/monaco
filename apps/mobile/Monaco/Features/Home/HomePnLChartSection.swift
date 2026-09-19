import Charts
import SwiftUI

/// A slim, chrome-free P&L strip — no title, no card, no axes. Real cabals have only a
/// few NAV snapshots, so this (and the caller) hide the whole section under 3 points
/// rather than show a scrub-able chart that would read as broken.
struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]

    private var chartTint: Color {
        guard let last = points.last else { return MonacoTheme.ink }
        return last.chartValue >= 0 ? MonacoTheme.profit : MonacoTheme.loss
    }

    var body: some View {
        Chart(points) { point in
            AreaMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(chartTint.opacity(0.12))
            LineMark(
                x: .value("Time", point.ts),
                y: .value("P&L", point.chartValue)
            )
            .foregroundStyle(chartTint)
            .lineStyle(StrokeStyle(lineWidth: 2))
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .frame(height: 120)
        .accessibilityIdentifier("home-pnl-chart")
    }
}
