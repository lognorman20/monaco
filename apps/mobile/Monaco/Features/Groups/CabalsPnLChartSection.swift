import Charts
import MonacoCore
import SwiftUI

/// One P&L line per cabal the viewer belongs to.
struct CabalsPnLChartSection: View {
    let model: CabalsTabModel
    let hasCabals: Bool

    /// Each line takes its cabal's identity tint, so a cabal is the same colour here as in
    /// its mark, its strip card and its hero. Gain/loss stays with the figures in the legend:
    /// on a multi-cabal comparison, colouring every line green or red would say nothing.
    private static func color(forGroupID groupID: String) -> Color {
        MonacoTheme.CabalTint.stroke(forGroupId: groupID)
    }

    private var drawable: [GroupPnLSeriesDTO] {
        GroupPnLChartModel.drawable(model.series)
    }

    /// A scrub-worthy line needs at least 3 points; the chart only earns its
    /// place once two or more cabals clear that bar. Below it, the chart is
    /// hidden entirely rather than showing an empty card.
    private var hasEnoughData: Bool {
        drawable.filter { $0.points.count >= 3 }.count >= 2
    }

    /// While loading or recovering from an error we still want feedback, but
    /// a genuinely sparse chart (or no cabals at all) renders nothing.
    private var shouldRender: Bool {
        guard hasCabals else { return false }
        if model.isChartLoading, model.series.isEmpty { return true }
        if model.chartFailed, model.series.isEmpty { return true }
        return hasEnoughData
    }

    var body: some View {
        if shouldRender {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text("Your cabals' P&L")
                    .font(MonacoTheme.Typo.section)
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
        if model.isChartLoading, model.series.isEmpty {
            ProgressView()
                .tint(MonacoTheme.ink)
                .accessibilityIdentifier("cabals-pnl-loading")
        } else if model.chartFailed, model.series.isEmpty {
            emptyMessage("Couldn't load the chart. Pull down to try again.", id: "cabals-pnl-error")
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
                ForEach(series, id: \.id) { line in
                    ForEach(line.points) { point in
                        LineMark(
                            x: .value("Time", point.at),
                            y: .value("P&L", point.chartValue),
                            series: .value("Cabal", line.groupID)
                        )
                        .foregroundStyle(Self.color(forGroupID: line.groupID))
                        .interpolationMethod(.monotone)
                        .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                    }
                }
            }
            .chartLegend(.hidden)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 3)) { _ in
                    AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                        .foregroundStyle(MonacoTheme.tertiaryText)
                }
            }
            .chartYAxis(.hidden)
            .frame(height: 180)
            .accessibilityIdentifier("cabals-pnl-chart")

            legend(series: series)
        }
    }

    private func legend(series: [GroupPnLSeriesDTO]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            ForEach(series, id: \.id) { line in
                HStack(spacing: MonacoTheme.Space.s) {
                    Capsule()
                        .fill(Self.color(forGroupID: line.groupID))
                        .frame(width: 18, height: 3)
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
