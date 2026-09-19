import Charts
import MonacoCore
import SwiftUI

/// One P&L line per cabal the viewer belongs to.
struct CabalsPnLChartSection: View {
    let model: CabalsTabModel
    let hasCabals: Bool

    /// The theme is monochrome and reserves green/red for P&L figures, so
    /// series differ by gray tone and dash pattern instead of hue.
    private struct SeriesStyle {
        let color: Color
        let dash: [CGFloat]
    }

    private static let styles: [SeriesStyle] = [
        SeriesStyle(color: MonacoTheme.ink, dash: []),
        SeriesStyle(color: MonacoTheme.muted, dash: [6, 4]),
        SeriesStyle(color: MonacoTheme.ink, dash: [2, 3]),
        SeriesStyle(color: MonacoTheme.muted, dash: []),
        SeriesStyle(color: MonacoTheme.disabled, dash: [8, 3, 2, 3]),
    ]

    private static func style(at index: Int) -> SeriesStyle {
        styles[index % styles.count]
    }

    private var drawable: [GroupPnLSeriesDTO] {
        GroupPnLChartModel.drawable(model.series)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Your cabals' P&L")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)
            rangePicker

            MonacoCard {
                content
                    .frame(maxWidth: .infinity, minHeight: 180)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("cabals-pnl-section")
    }

    private var rangePicker: some View {
        HStack(spacing: 6) {
            ForEach(GroupPnLRange.allCases, id: \.self) { option in
                Button {
                    model.selectRange(option)
                } label: {
                    MonacoChip(title: option.label, isSelected: model.range == option)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("cabals-pnl-range-\(option.rawValue)")
                .accessibilityAddTraits(model.range == option ? .isSelected : [])
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if model.isChartLoading {
            ProgressView()
                .tint(MonacoTheme.ink)
                .accessibilityIdentifier("cabals-pnl-loading")
        } else if !hasCabals {
            emptyMessage("Join or create a cabal to chart its gains here.", id: "cabals-pnl-empty")
        } else if model.chartFailed, model.series.isEmpty {
            emptyMessage("Couldn't load the chart. Pull down to try again.", id: "cabals-pnl-error")
        } else if drawable.isEmpty {
            emptyMessage("Lines appear once your cabals add money and trade.", id: "cabals-pnl-sparse")
        } else {
            chart
        }
    }

    private func emptyMessage(_ text: String, id: String) -> some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "chart.xyaxis.line")
                .font(.title2)
                .foregroundStyle(MonacoTheme.muted)
            Text(text)
                .font(MonacoTheme.TypeRole.body)
                .foregroundStyle(MonacoTheme.muted)
                .multilineTextAlignment(.center)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(id)
    }

    private var chart: some View {
        let series = drawable
        return VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Chart {
                RuleMark(y: .value("Break even", 0))
                    .foregroundStyle(MonacoTheme.hairline)
                    .lineStyle(StrokeStyle(lineWidth: 1))
                ForEach(Array(series.enumerated()), id: \.element.id) { index, line in
                    let style = Self.style(at: index)
                    ForEach(line.points) { point in
                        LineMark(
                            x: .value("Time", point.at),
                            y: .value("P&L", point.chartValue),
                            series: .value("Cabal", line.groupID)
                        )
                        .foregroundStyle(style.color)
                        .interpolationMethod(.monotone)
                        .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, dash: style.dash))
                    }
                }
            }
            .chartLegend(.hidden)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 3)) { _ in
                    AxisGridLine().foregroundStyle(MonacoTheme.hairline.opacity(0.5))
                    AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                }
            }
            .chartYAxis {
                AxisMarks(position: .leading, values: .automatic(desiredCount: 3)) { value in
                    AxisGridLine().foregroundStyle(MonacoTheme.hairline.opacity(0.5))
                    AxisValueLabel {
                        if let amount = value.as(Double.self) {
                            Text(amount, format: .currency(code: "USD").precision(.fractionLength(0)))
                        }
                    }
                }
            }
            .frame(height: 180)
            .accessibilityIdentifier("cabals-pnl-chart")

            legend(series: series)
        }
    }

    private func legend(series: [GroupPnLSeriesDTO]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(series.enumerated()), id: \.element.id) { index, line in
                let style = Self.style(at: index)
                HStack(spacing: MonacoTheme.Space.s) {
                    Path { path in
                        path.move(to: CGPoint(x: 0, y: 2))
                        path.addLine(to: CGPoint(x: 18, y: 2))
                    }
                    .stroke(style.color, style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: style.dash))
                    .frame(width: 18, height: 4)
                    Text(line.name)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                    Spacer()
                    if let last = line.points.last {
                        Text(SignedUsdFormatter.format(last.dollarPnl))
                            .font(MonacoTheme.TypeRole.caption.monospacedDigit())
                            .foregroundStyle(SignedUsdFormatter.isLoss(last.dollarPnl) ? MonacoTheme.loss : MonacoTheme.profit)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("cabals-pnl-legend-\(line.groupID)")
            }
        }
    }
}
