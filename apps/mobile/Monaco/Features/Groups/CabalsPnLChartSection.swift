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

/// One P&L line per cabal the viewer belongs to, on the tab's one ink band.
///
/// Ink is where money is held, and this is the only full-bleed object on the Cabals tab — which
/// is what makes it read as the screen's subject rather than another card. The tinted lines are
/// drawn on `Ink.sunken` so they glow; every line carries its cabal's name at its right terminus,
/// so colour is never the only thing telling two cabals apart.
struct CabalsPnLChartSection: View {
    let model: CabalsTabModel
    let hasCabals: Bool
    /// Resolved across the viewer's own cabals, so a line here is the same colour as that
    /// cabal's mark, its strip card band and its hero.
    var tints: [String: MonacoTheme.CabalTint] = [:]

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private func color(forGroupID groupID: String) -> Color {
        // `onInk` rather than `stroke`: this plot is deep ink in both schemes, so the light-mode
        // fill would be reading against the wrong surface half the time.
        CabalTintAssignment.tint(forGroupId: groupID, in: tints).onInk
    }

    private var drawable: [GroupPnLSeriesDTO] {
        GroupPnLChartModel.drawable(model.series)
    }

    /// A scrub-worthy line needs at least 3 points; a comparison needs two
    /// cabals that clear that bar.
    private var hasEnoughData: Bool {
        CabalsTabModel.isChartable(model.series)
    }

    private var rangeSelection: Binding<GroupPnLRange> {
        Binding(get: { model.range }, set: { model.selectRange($0) })
    }

    var body: some View {
        if model.showsChartSection(hasCabals: hasCabals) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Your cabals")
                        .displayFont(.eyebrow)
                        .foregroundStyle(MonacoTheme.Ink.fgSubtle)
                    Text("P&L")
                        .displayFont(.title)
                        .foregroundStyle(MonacoTheme.Ink.fgPrimary)
                        .accessibilityAddTraits(.isHeader)
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Your cabals' P&L")

                MonacoSegmented(GroupPnLRange.allCases, selection: rangeSelection) { $0.label }
                    .accessibilityIdentifier("cabals-pnl-range")

                content
                    .frame(maxWidth: .infinity, minHeight: 200)
            }
            .monacoInkBand()
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("cabals-pnl-section")
        }
    }

    @ViewBuilder
    private var content: some View {
        if model.isChartLoading {
            ProgressView()
                .tint(MonacoTheme.Ink.fgPrimary)
                .frame(maxWidth: .infinity, minHeight: 200)
                .accessibilityIdentifier("cabals-pnl-loading")
        } else if model.chartFailed, model.series.isEmpty {
            emptyMessage("Couldn't load the chart. Pull down to try again.", id: "cabals-pnl-error")
        } else if !hasEnoughData {
            emptyMessage("Not enough history in \(model.range.spokenWindow) yet. Try a longer stretch.", id: "cabals-pnl-sparse")
        } else {
            chart
                // A range switch keeps the old lines on screen; dim them so the
                // highlighted segment and the drawing agree about what is showing.
                .opacity(model.isChartReloading ? 0.4 : 1)
                .animation(MonacoMotion.glide.reduced(reduceMotion), value: model.isChartReloading)
        }
    }

    private func emptyMessage(_ text: String, id: String) -> some View {
        VStack(spacing: MonacoTheme.Space.s) {
            Image(systemName: "chart.xyaxis.line")
                .font(.title2)
                .foregroundStyle(MonacoTheme.Ink.fgSubtle)
            Text(text)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.Ink.fgMuted)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, minHeight: 200)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(id)
    }

    private var chart: some View {
        let series = drawable
        return Chart {
            RuleMark(y: .value("Break even", 0))
                .foregroundStyle(MonacoTheme.Ink.lineStrong)
                .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
            ForEach(series, id: \.id) { line in
                ForEach(line.points) { point in
                    LineMark(
                        x: .value("Time", point.at),
                        y: .value("P&L", point.chartValue),
                        series: .value("Cabal", line.groupID)
                    )
                    .foregroundStyle(color(forGroupID: line.groupID))
                    .interpolationMethod(.monotone)
                    .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                }
                // The terminus carries the name. The hand-built swatch legend is gone: a legend
                // makes colour the only identity carrier and then asks the reader to hold seven
                // of them in their head.
                if let last = line.points.last {
                    PointMark(
                        x: .value("Time", last.at),
                        y: .value("P&L", last.chartValue)
                    )
                    .foregroundStyle(color(forGroupID: line.groupID))
                    .symbolSize(28)
                    .annotation(position: .trailing, alignment: .leading, spacing: 4) {
                        Text(line.name)
                            .font(MonacoTheme.Typo.micro)
                            .foregroundStyle(color(forGroupID: line.groupID))
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .frame(maxWidth: 88, alignment: .leading)
                            .accessibilityHidden(true)
                    }
                }
            }
        }
        .chartLegend(.hidden)
        .chartXAxis {
            AxisMarks(values: .automatic(desiredCount: 3)) { _ in
                AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                    .foregroundStyle(MonacoTheme.Ink.fgSubtle)
            }
        }
        .chartYAxis(.hidden)
        .chartPlotStyle { plot in
            plot
                .background(MonacoTheme.Ink.sunken)
                .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
        }
        .frame(height: 200)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Self.chartLabel(series: series, range: model.range))
        .accessibilityIdentifier("cabals-pnl-chart")
    }

    /// The whole chart in one sentence, because the lines themselves carry nothing to VoiceOver.
    static func chartLabel(series: [GroupPnLSeriesDTO], range: GroupPnLRange) -> String {
        let lines = series.compactMap { line -> String? in
            guard let last = line.points.last else { return nil }
            return "\(line.name) \(PnLSpeech.dollars(last.dollarPnl))"
        }
        guard !lines.isEmpty else { return "Your cabals' P&L over \(range.spokenWindow)" }
        return "Your cabals' P&L over \(range.spokenWindow). " + lines.joined(separator: ", ")
    }
}
