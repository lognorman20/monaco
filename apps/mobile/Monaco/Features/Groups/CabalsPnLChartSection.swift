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

    /// The plot area's height, pinned rather than inferred, so the terminus labels can be placed
    /// in points from a value-space position without guessing what the x-axis left over.
    private static let plotHeight: CGFloat = 176

    /// How much of the plot's width is reserved to the right of the last point for the name
    /// labels. A terminus sits at the plot's right edge by definition, so without this the label
    /// has nowhere to go and `overflowResolution` would drag it back over its own line.
    private static let labelWidth: CGFloat = 72
    private static let labelGutter: CGFloat = 76

    /// Explicit rather than inferred, because `labelOffsets` maps a value to a point inside it.
    /// Zero is always in range: the break-even rule is drawn at zero and a chart that hides it is
    /// a chart that does not say whether a cabal is up.
    private var yDomain: ClosedRange<Double> {
        // Typed: `[Double] + [0]` infers `[Any]`, and every comparison below then fails.
        var values: [Double] = drawable.flatMap { $0.points.map(\.chartValue) }
        values.append(0)
        guard let low = values.min(), let high = values.max() else { return -1...1 }
        guard high > low else { return (low - 1)...(high + 1) }
        let pad = (high - low) * 0.12
        return (low - pad)...(high + pad)
    }

    /// Where each cabal's name label sits, in points from its own terminus.
    ///
    /// Every line ends at the same instant, so the labels share one column, and the name is the
    /// only non-colour identity carrier on this chart (§1.8) — two cabals with similar P&L must
    /// not print their names on top of each other. Two passes:
    ///
    /// 1. Top down, each label pushed just far enough below the one above it to clear it. The
    ///    topmost does not move, so a name is always nearest the line it belongs to.
    /// 2. If that pushed the lowest label past the bottom of the plot, walk back up until the
    ///    stack ends inside it, then bring the whole stack down again if it has cleared the top.
    ///    With seven cabals at 15pt apart the stack is 90pt in a 176pt plot, so it cannot
    ///    overflow both ends.
    static func labelOffsets(
        terminals: [(id: String, value: Double)],
        domain: ClosedRange<Double>,
        plotHeight: CGFloat,
        minimumSeparation: CGFloat = 15
    ) -> [String: CGFloat] {
        let span = domain.upperBound - domain.lowerBound
        guard span > 0, !terminals.isEmpty else { return [:] }
        // Value space runs up, point space runs down.
        func y(_ value: Double) -> CGFloat {
            CGFloat((domain.upperBound - value) / span) * plotHeight
        }

        let ordered = terminals.sorted { $0.value > $1.value }
        let wanted = ordered.map { y($0.value) }
        var placed = wanted
        for index in placed.indices.dropFirst() {
            placed[index] = max(placed[index], placed[index - 1] + minimumSeparation)
        }

        let bottom = plotHeight - minimumSeparation / 2
        let top = minimumSeparation / 2
        if let last = placed.last, last > bottom {
            placed[placed.count - 1] = bottom
            for index in placed.indices.dropLast().reversed() {
                placed[index] = min(placed[index], placed[index + 1] - minimumSeparation)
            }
            if let first = placed.first, first < top {
                let shift = top - first
                for index in placed.indices { placed[index] += shift }
            }
        }

        var offsets: [String: CGFloat] = [:]
        for (index, terminal) in ordered.enumerated() {
            offsets[terminal.id] = placed[index] - wanted[index]
        }
        return offsets
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

                InkSegmented(
                    GroupPnLRange.allCases,
                    selection: rangeSelection,
                    identifierPrefix: "cabals-pnl-range"
                ) { $0.label }
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
        let domain = yDomain
        let offsets = Self.labelOffsets(
            terminals: series.compactMap { line in
                line.points.last.map { (id: line.groupID, value: $0.chartValue) }
            },
            domain: domain,
            plotHeight: Self.plotHeight
        )
        return Chart {
            RuleMark(y: .value("Break even", 0.0))
                .foregroundStyle(MonacoTheme.Ink.lineStrong)
                .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
            ForEach(series, id: \.id) { line in
                lineMarks(for: line)
                // The terminus stays inside the `Chart` builder: lifted into a function of its
                // own, `PointMark(x:y:)` resolves to the 3D chart's mark, which has no
                // `annotation`. The label and the line marks are what the type checker needed
                // taking out of here, and both are below.
                ForEach(terminals(of: line), id: \.id) { last in
                    PointMark(
                        x: .value("Time", last.at),
                        y: .value("P&L", last.chartValue)
                    )
                    .foregroundStyle(color(forGroupID: line.groupID))
                    .symbolSize(28)
                    .annotation(
                        position: .trailing,
                        alignment: .leading,
                        spacing: 4,
                        overflowResolution: .init(x: .fitToChart, y: .fitToChart)
                    ) {
                        terminusLabel(line, offset: offsets[line.groupID] ?? 0)
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
        .chartYScale(domain: domain)
        .chartXScale(range: .plotDimension(endPadding: Self.labelGutter))
        .chartPlotStyle { plot in
            plot
                .frame(height: Self.plotHeight)
                .background(MonacoTheme.Ink.sunken)
                .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.container, style: .continuous))
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Self.chartLabel(series: series, range: model.range))
        .accessibilityIdentifier("cabals-pnl-chart")
    }

    /// One cabal's line. Split out of the `Chart` builder because the whole thing in one
    /// expression is past what the type checker will do in reasonable time.
    @ChartContentBuilder
    private func lineMarks(for line: GroupPnLSeriesDTO) -> some ChartContent {
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
    }

    /// The last point of a line, as a zero-or-one collection so the `Chart` builder can walk it
    /// with a `ForEach` instead of an `if let` — which is what keeps the mark below inside the
    /// two-dimensional chart's world.
    private func terminals(of line: GroupPnLSeriesDTO) -> [GroupPnLPointDTO] {
        line.points.last.map { [$0] } ?? []
    }

    /// The name at the end of one cabal's line.
    ///
    /// The hand-built swatch legend is gone: a legend makes colour the only identity carrier and
    /// then asks the reader to hold seven of them in their head. A terminus sits at the plot's
    /// right edge by definition, so the label needs both somewhere to go — the gutter reserved on
    /// the x scale — and an overflow rule, or the chart clips its own only non-colour signal.
    private func terminusLabel(_ line: GroupPnLSeriesDTO, offset: CGFloat) -> some View {
        Text(line.name)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(color(forGroupID: line.groupID))
            .lineLimit(1)
            .truncationMode(.tail)
            .frame(maxWidth: Self.labelWidth, alignment: .leading)
            .offset(y: offset)
            .accessibilityHidden(true)
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


/// The range control on the tab's ink band.
///
/// `MonacoSegmented` fills its track with `MonacoTheme.surfaceSunken`, which §1.2 aliases to
/// `fillQuiet` — `#EDF1F7` in light. That is a near-white capsule dropped onto `Ink.base`, and
/// §5.2.2 asks for an `Ink.sunken` track here. Making `MonacoSegmented` read `\.monacoWorld` is
/// Chunk B's line in §7 and this file may not touch it, so this is the ink-world twin of that
/// control, built from the same tokens and the same behaviour: `snap` on the thumb, a selection
/// haptic, the `.isSelected` trait and a 44pt target.
///
/// **It goes away the moment Chunk B lands.** When `MonacoSegmented` reads the world, delete this
/// and put `MonacoSegmented` back — the shape and the identifiers are deliberately identical so
/// that is a one-line swap.
private struct InkSegmented<T: Hashable>: View {
    private let options: [T]
    @Binding private var selection: T
    private let label: (T) -> String
    /// Each option is addressable as `<prefix>-<label>`. An identifier on the container alone is
    /// the container's, not its buttons', so without this a test can see the control but cannot
    /// tap a range in it.
    private let identifierPrefix: String?

    @Namespace private var thumb
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(
        _ options: [T],
        selection: Binding<T>,
        identifierPrefix: String? = nil,
        label: @escaping (T) -> String
    ) {
        self.options = options
        _selection = selection
        self.identifierPrefix = identifierPrefix
        self.label = label
    }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(options, id: \.self) { option in
                let isSelected = option == selection
                Button {
                    guard option != selection else { return }
                    Haptics.selection()
                    withAnimation(MonacoMotion.snap.reduced(reduceMotion)) {
                        selection = option
                    }
                } label: {
                    Text(label(option))
                        .font(MonacoTheme.Typo.callout.weight(.semibold))
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                        // Blue means tap, on ink as on paper: the thumb is the brand fill and the
                        // cabal's tint never reaches a control (§1.8).
                        .foregroundStyle(isSelected ? MonacoTheme.onBrand : MonacoTheme.Ink.fgMuted)
                        .padding(.horizontal, 12)
                        .frame(maxWidth: .infinity, minHeight: 36)
                        .background {
                            if isSelected {
                                Capsule()
                                    .fill(MonacoTheme.brandFill)
                                    .matchedGeometryEffect(id: "thumb", in: thumb)
                            }
                        }
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(isSelected ? [.isSelected] : [])
                .modifier(OptionalIdentifier(identifierPrefix.map { "\($0)-\(label(option))" }))
            }
        }
        .padding(4)
        .frame(minHeight: 44)
        .background(Capsule().fill(MonacoTheme.Ink.sunken))
    }
}


/// Applies an accessibility identifier only when there is one to apply, so a caller that passes
/// none is left exactly as it was rather than being handed an empty string.
private struct OptionalIdentifier: ViewModifier {
    let identifier: String?

    init(_ identifier: String?) { self.identifier = identifier }

    @ViewBuilder
    func body(content: Content) -> some View {
        if let identifier {
            content.accessibilityIdentifier(identifier)
        } else {
            content
        }
    }
}
