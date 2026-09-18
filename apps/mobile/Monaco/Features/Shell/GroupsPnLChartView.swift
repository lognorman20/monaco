import Charts
import SwiftUI

struct GroupsPnLChartView: View {
    let series: [GroupPnLChartSeries]

    var body: some View {
        if series.isEmpty || series.allSatisfy({ $0.points.isEmpty }) {
            MonacoEmptyStateCard(
                message: "P&L history appears after your cabals have NAV snapshots.",
                systemImage: "chart.line.uptrend.xyaxis"
            )
        } else {
            VStack(alignment: .leading, spacing: 12) {
                Chart {
                    ForEach(series) { groupSeries in
                        ForEach(groupSeries.points) { point in
                            LineMark(
                                x: .value("Date", point.date),
                                y: .value("Pot", point.potValueUsd)
                            )
                            .foregroundStyle(by: .value("Cabal", groupSeries.name))
                            .interpolationMethod(.catmullRom)
                        }
                    }
                }
                .chartForegroundStyleScale(range: chartColors)
                .frame(height: 180)
                .chartXAxis {
                    AxisMarks(values: .automatic(desiredCount: 4))
                }
                .chartYAxis {
                    AxisMarks(position: .leading)
                }

                chartLegend
            }
            .padding(.vertical, 4)
        }
    }

    private var chartLegend: some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(series.filter { !$0.points.isEmpty }) { groupSeries in
                HStack(spacing: 8) {
                    Circle()
                        .fill(chartColor(for: groupSeries.name))
                        .frame(width: 8, height: 8)
                    Text(groupSeries.name)
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.primaryText)
                    Spacer()
                }
            }
        }
    }

    private var chartColors: [Color] {
        [
            MonacoTheme.accent,
            MonacoTheme.success,
            MonacoTheme.warning,
            Color.blue,
            Color.purple,
            Color.teal,
        ]
    }

    private func chartColor(for name: String) -> Color {
        let names = series.map(\.name)
        guard let index = names.firstIndex(of: name) else {
            return MonacoTheme.accent
        }
        return chartColors[index % chartColors.count]
    }
}
