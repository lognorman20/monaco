import Charts
import SwiftUI

struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]

    private var chartTint: Color {
        guard let last = points.last else { return MonacoTheme.ink }
        return last.chartValue >= 0 ? MonacoTheme.profit : MonacoTheme.loss
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("P&L · last hour")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

            if points.count < 2 {
                MonacoEmptyStateCard(
                    message: "P&L history shows up after you fund a cabal.",
                    systemImage: "chart.line.uptrend.xyaxis"
                )
            } else {
                Chart(points) { point in
                    AreaMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(chartTint.opacity(0.18))
                    LineMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(chartTint)
                    .lineStyle(StrokeStyle(lineWidth: 2))
                }
                .chartXAxis(.hidden)
                .chartYAxis {
                    AxisMarks(position: .leading, values: .automatic(desiredCount: 3))
                }
                .frame(height: 160)
                .accessibilityIdentifier("home-pnl-chart")
            }
        }
    }
}
