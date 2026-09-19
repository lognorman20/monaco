import Charts
import SwiftUI

struct HomePnLChartSection: View {
    let points: [HomePnLSeriesPointDTO]

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
                    .foregroundStyle(MonacoTheme.accent.opacity(0.18))
                    LineMark(
                        x: .value("Time", point.ts),
                        y: .value("P&L", point.chartValue)
                    )
                    .foregroundStyle(MonacoTheme.accent)
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
