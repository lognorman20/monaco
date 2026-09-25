import Charts
import MonacoCore
import SwiftUI

extension GroupPnLRange {
    /// The range as it is spoken inside a sentence. `label` is the chip code
    /// ("1D"), which is right on a chip and reads as jargon in copy.
    var spokenWindow: String {
        switch self {
        case .oneDay: "the last day"
        case .oneWeek: "the last week"
        case .oneMonth: "the last month"
        case .threeMonths: "the last three months"
        }
    }
}

/// One return line per cabal the viewer belongs to, on the paper, with the cabals as a
/// ruled legend under it.
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

    /// A scrub-worthy line needs at least 3 points; a comparison needs two
    /// cabals that clear that bar.
    private var hasEnoughData: Bool {
        CabalsTabModel.isChartable(model.series)
    }

    var body: some View {
        if model.showsChartSection(hasCabals: hasCabals) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader("Your cabals' return")
                    rangePicker
                }
                .padding(.horizontal, MonacoTheme.Space.m)

                content
                    .frame(maxWidth: .infinity, minHeight: 180)
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("cabals-pnl-section")
        }
    }

    private var rangePicker: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            ForEach(GroupPnLRange.allCases, id: \.self) { option in
                let isSelected = model.range == option
                Button {
                    model.selectRange(option)
                } label: {
                    Text(option.label)
                        .font(MonacoTheme.Typo.dataCaption)
                        .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.muted)
                        .padding(.horizontal, 14)
                        .frame(minWidth: 48, minHeight: 34)
                        .background(Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
                        .padding(.vertical, 5)
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(option.spokenWindow)
                .accessibilityIdentifier("cabals-pnl-range-\(option.rawValue)")
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if model.isChartLoading {
            ProgressView()
                .tint(MonacoTheme.ink)
                .accessibilityIdentifier("cabals-pnl-loading")
        } else if model.chartFailed, model.series.isEmpty {
            emptyMessage("Couldn't load the chart. Pull down to try again.", id: "cabals-pnl-error")
        } else if !hasEnoughData {
            emptyMessage("Not enough history in \(model.range.spokenWindow) yet. Try a longer stretch.", id: "cabals-pnl-sparse")
        } else {
            chart
                // A range switch keeps the old lines on screen; dim them so the
                // highlighted chip and the drawing agree about what is showing.
                .opacity(model.isChartReloading ? 0.4 : 1)
                .animation(.easeInOut(duration: 0.15), value: model.isChartReloading)
        }
    }

    private func emptyMessage(_ text: String, id: String) -> some View {
        Text(text)
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.muted)
            .multilineTextAlignment(.center)
            .padding(.horizontal, MonacoTheme.Space.l)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier(id)
    }

    private var chart: some View {
        let series = drawable
        return VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
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
                        .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                    }
                }
            }
            .chartLegend(.hidden)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 3)) { _ in
                    AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                        .font(MonacoTheme.Typo.stamp)
                        .foregroundStyle(MonacoTheme.tertiaryText)
                }
            }
            .chartYAxis(.hidden)
            .frame(height: 160)
            .padding(.horizontal, MonacoTheme.Space.m)
            .accessibilityIdentifier("cabals-pnl-chart")

            legend(series: series)
        }
    }

    /// The cabals as ruled rows: a swatch in the line's colour, the name, and where the line
    /// ends in the window.
    private func legend(series: [GroupPnLSeriesDTO]) -> some View {
        MonacoGroupedList {
            ForEach(Array(series.enumerated()), id: \.element.id) { index, line in
                HStack(spacing: MonacoTheme.Space.sm) {
                    Capsule()
                        .fill(Self.color(forGroupID: line.groupID))
                        .frame(width: 18, height: 3)
                    Text(line.name)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(1)
                    Spacer()
                    if let last = line.points.last {
                        PnLText(dollarPnl: last.dollarPnl, style: .row)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .frame(minHeight: 44)
                .overlay(alignment: .bottom) {
                    if index < series.count - 1 {
                        MonacoRule().padding(.leading, MonacoTheme.Space.m + 18 + MonacoTheme.Space.sm)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("cabals-pnl-legend-\(line.groupID)")
            }
        }
    }
}
