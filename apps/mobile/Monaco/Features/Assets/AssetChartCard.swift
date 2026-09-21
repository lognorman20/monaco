import MonacoCore
import SwiftUI

/// The curve, what it is a curve of, and the range chips under it.
///
/// Chips sit below the chart, where every broker puts them: the chart is what the
/// thumb reaches for, and a row of controls above it is a row of things to hit by
/// accident while scrubbing.
struct AssetChartCard: View {
    @Bindable var model: AssetDetailModel
    /// True while the exchange behind the curve is still printing. Only the day chart
    /// uses it — a pulsing dot on a year of history claims a liveness nobody means.
    let isMarketLive: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            chart
                .frame(height: Self.chartHeight)
            if let caption = model.series?.basisCaption {
                // The curve is the underlying equity's; the price above it is the
                // token's. Say so, or a curve ending below the hero price reads as a
                // bug instead of as the premium it is.
                Text(caption)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .accessibilityIdentifier("asset-chart-basis")
            }
            rangeChips
        }
        .accessibilityIdentifier("asset-detail-chart-section")
    }

    static let chartHeight: CGFloat = 200

    // MARK: - The curve

    @ViewBuilder
    private var chart: some View {
        switch model.chartState {
        case .loading:
            SkeletonBlock(width: nil, height: Self.chartHeight, radius: MonacoTheme.Radius.card)
                .frame(maxWidth: .infinity)
                .accessibilityLabel("Loading price history")
                .accessibilityIdentifier("asset-detail-chart-loading")
        case .series(let series):
            curve(series)
        case .empty:
            EmptyState(
                title: "No price history for this window yet",
                message: "Try another range, or ask again.",
                actionTitle: "Try again",
                action: { reload() }
            )
            .accessibilityIdentifier("asset-detail-chart-empty")
        case .failed:
            EmptyState(
                title: "Could not load price history",
                actionTitle: "Retry",
                action: { reload() }
            )
            .accessibilityIdentifier("asset-detail-chart-failed")
        }
    }

    private func curve(_ series: AssetChartSeries) -> some View {
        MonacoScrubChart(
            points: series.points.map { MonacoScrubChart.Point(date: $0.date, value: $0.chartValue) },
            tint: tint,
            baseline: series.drawsBaselineRule ? series.baselineValue : nil,
            height: Self.chartHeight,
            isLive: isMarketLive && series.range == .oneDay,
            drawOnKey: Self.drawOnKey(series),
            selection: $model.scrubbedIndex,
            summary: summary(series),
            describePoint: { describe(series, at: $0) },
            accessibilityIdentifier: "asset-detail-chart"
        )
    }

    /// What makes the draw-on replay: a different range, or a genuinely different
    /// series. A quiet re-read that returns the same bars must not redraw the curve
    /// under the member's finger.
    private static func drawOnKey(_ series: AssetChartSeries) -> AnyHashable {
        [
            series.range.rawValue,
            String(series.points.count),
            String(series.points.first?.timestamp ?? 0),
            String(series.points.last?.timestamp ?? 0),
        ]
    }

    /// The figure's own verdict, not a second one computed from the series: a move the
    /// header prints as flat must not be drawn as a gain.
    private var tint: Color {
        switch model.curveDirection {
        case .up: return MonacoTheme.profitVivid
        case .down: return MonacoTheme.lossVivid
        case .flat: return MonacoTheme.muted
        }
    }

    private func reload() {
        Task { await model.loadChart(range: model.range) }
    }

    // MARK: - Range chips

    /// Six ranges do not fit on one line. At the default text size the chips are
    /// already ~320pt of the 335pt a 375pt device leaves inside the gutters, so one
    /// Dynamic Type step up clipped the row and an accessibility size made it
    /// unreadable. Scrolling horizontally is what Robinhood does at large text
    /// sizes, and it is the only layout that stays correct as the chips grow.
    ///
    /// `scrollClipDisabled` is off on purpose: the row must clip at the gutter so a
    /// half-visible chip reads as "there is more", rather than bleeding to the edge.
    private var rangeChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(AssetChartRange.allCases, id: \.self) { range in
                    AssetChartRangeChip(
                        range: range,
                        isSelected: model.range == range,
                        isLoading: model.loadingRanges.contains(range)
                    ) {
                        guard model.range != range else { return }
                        Haptics.selection()
                        model.range = range
                    }
                }
            }
            // The capsules have a stroke, so a hairline of padding keeps the first
            // and last chip from being shaved by the clip edge.
            .padding(.horizontal, 1)
        }
        .scrollIndicators(.hidden)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Chart range")
        .accessibilityIdentifier("asset-chart-ranges")
    }

    // MARK: - VoiceOver

    private func summary(_ series: AssetChartSeries) -> String {
        let move = model.move.map { PercentReturnFormatter.format($0.ratio) } ?? "—"
        var sentence = "\(series.range.accessibilityLabel) price history. "
            + "\(move) \(series.range.moveLabel.lowercased()). "
            + "Low \(UsdAmountFormatter.format(micros: micros(series.lowValue))), "
            + "high \(UsdAmountFormatter.format(micros: micros(series.highValue)))."
        if let caption = series.basisCaption {
            sentence += " \(caption)."
        }
        return sentence
    }

    private func describe(_ series: AssetChartSeries, at index: Int) -> String {
        guard let point = series.point(at: index) else { return "—" }
        let price = UsdAmountFormatter.format(micros: point.priceUsdcMicros)
        let time = ChartScrubLabel.caption(for: point.date, range: series.range)
        guard let ratio = series.changeRatio(toIndex: index) else { return "\(price), \(time)" }
        return "\(price), \(PnLSpeech.percent(PercentReturnFormatter.format(ratio))), \(time)"
    }

    private func micros(_ value: Double) -> Int64 {
        Int64((value * 1_000_000).rounded())
    }
}

/// A range chip. 44pt tall so it is a real target, and it carries its own spinner:
/// a range that is still loading used to look exactly like a range that had arrived,
/// which made a slow fetch read as a chart that had not changed.
struct AssetChartRangeChip: View {
    let range: AssetChartRange
    let isSelected: Bool
    let isLoading: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(range.label)
                    .font(MonacoTheme.Typo.callout.weight(.semibold))
                    .lineLimit(1)
                if isLoading {
                    ProgressView()
                        .controlSize(.mini)
                        .tint(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.muted)
                }
            }
            .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
            .padding(.horizontal, 14)
            .frame(minHeight: 44)
            .background(Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(range.accessibilityLabel)
        .accessibilityValue(isLoading ? "Loading" : "")
        .accessibilityAddTraits(isSelected ? [.isSelected] : [])
        .accessibilityIdentifier("asset-chart-range-\(range.rawValue)")
    }
}
