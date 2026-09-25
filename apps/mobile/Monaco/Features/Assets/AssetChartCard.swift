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

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            // No height here. Only a curve and its skeleton are 200pt tall; an empty
            // or failed state is a title, a message and a button, and forcing that
            // into a fixed height does not clip it — it overflows, and at the
            // accessibility text sizes the retry button draws straight over the
            // caption and the chip row below.
            chart
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
        // No identifier on this stack. A modifier on a `VStack` is applied to each of
        // its children, so an identifier here is not a name for the group: it renames
        // the curve, the caption and the chip row, and every one of them becomes
        // unfindable by the name it was actually given.
    }

    static let chartHeight: CGFloat = 200

    // MARK: - The curve

    @ViewBuilder
    private var chart: some View {
        switch model.chartState {
        case .loading:
            SkeletonBlock(width: nil, height: Self.chartHeight, radius: MonacoTheme.Radius.card)
                .frame(maxWidth: .infinity)
                // `SkeletonBlock` hides itself from VoiceOver, so a label on the
                // outside would attach to nothing and the loading chart would
                // announce silence. Making this one element first is what gives the
                // label — and the identifier a UI test asks by — something to sit on.
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Loading price history")
                .accessibilityIdentifier("asset-detail-chart-loading")
        case .series(let series):
            curve(series)
        case .empty:
            EmptyState(
                title: model.detail?.resolvedKind == .preIpo ? PreIpoCopy.chartEmpty : "No price history for this window yet",
                message: "Try another range, or ask again.",
                actionTitle: "Try again",
                action: { reload() }
            )
            .frame(minHeight: Self.chartHeight)
            .accessibilityIdentifier("asset-detail-chart-empty")
        case .failed:
            EmptyState(
                title: "Could not load price history",
                actionTitle: "Retry",
                action: { reload() }
            )
            .frame(minHeight: Self.chartHeight)
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
            // The one nearest-sample search, not a second copy inside the chart: the
            // drag and the tests have to be measuring the same thing.
            nearestIndex: { series.nearestIndex(to: $0) },
            describePoint: { describe(series, at: $0) },
            accessibilityIdentifier: "asset-detail-chart"
        )
    }

    /// What makes the draw-on replay: a different window, and nothing else.
    ///
    /// It used to include the data's shape — the sample count and the first and last
    /// timestamps — which sounds like "a genuinely different series" and is in fact
    /// "a live day chart". The background re-read runs every two minutes and the last
    /// bar's timestamp advances on essentially every quiet re-read of an open market,
    /// so the curve wiped itself left to right every two minutes, unasked, including
    /// under a member's finger: the selection survives a replay but the mask does not.
    ///
    /// The first series for a window still draws on, because the chart view plays it
    /// on the first appearance of the curve rather than on this key.
    static func drawOnKey(_ series: AssetChartSeries) -> AnyHashable {
        series.range.rawValue
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
        ScrollViewReader { proxy in
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
                        .id(range)
                    }
                }
                // The capsules have a stroke, so a hairline of padding keeps the first
                // and last chip from being shaved by the clip edge.
                .padding(.horizontal, 1)
            }
            .scrollIndicators(.hidden)
            // A row that clips at the gutter can hold the selection off screen —
            // arriving on ALL, or coming back to a screen left on 1Y, would show six
            // chips with no visible selection at all. Bring it into the middle.
            .onAppear { proxy.scrollTo(model.range, anchor: .center) }
            .onChange(of: model.range) { _, range in
                guard !reduceMotion else {
                    proxy.scrollTo(range, anchor: .center)
                    return
                }
                withAnimation(.snappy(duration: 0.25)) { proxy.scrollTo(range, anchor: .center) }
            }
        }
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
///
/// Not built on `MonacoChip`: that primitive is deprecated in favour of exactly this
/// — "a 44pt chip built on surfaceSunken" — because its 8pt vertical padding leaves a
/// target under the 44pt minimum. Extending a deprecated view would spread it.
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
