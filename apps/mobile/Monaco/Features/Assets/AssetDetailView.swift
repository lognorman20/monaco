import Charts
import MonacoCore
import SwiftUI

struct AssetDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let symbol: String

    @State private var model: AssetDetailModel
    @State private var pickerKind: ProposalPickKind?
    @State private var toast: MonacoToast?

    init(auth: PrivyAuthService, symbol: String, dataSource: AssetDetailDataSource? = nil) {
        self.auth = auth
        self.symbol = symbol
        _model = State(initialValue: AssetDetailModel(
            symbol: symbol,
            dataSource: dataSource ?? LiveAssetDetailDataSource(auth: auth)
        ))
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                switch model.detailState {
                case .loading:
                    headerSkeleton
                    chartSection
                case .failed:
                    EmptyState(
                        title: "Could not load this stock",
                        actionTitle: "Retry",
                        action: { Task { await model.loadDetail() } }
                    )
                    .accessibilityIdentifier("asset-detail-failed")
                    // The curve was decoupled from this call; throwing away a chart that did
                    // arrive would leave the screen emptier than before they were split.
                    chartSection
                case .loaded(let detail):
                    header(detail)
                    if !detail.liquidity.routable {
                        Text("Can't be bought right now.")
                            .font(MonacoTheme.TypeRole.caption)
                            .foregroundStyle(MonacoTheme.warning)
                            .accessibilityIdentifier("asset-detail-no-route")
                    }
                    chartSection
                    actionRow
                }
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        .monacoToast($toast)
        // Two independent loads: the curve does not wait on the (slow) detail call.
        .task { await model.loadDetail() }
        .task(id: model.range) { await model.loadChart(range: model.range) }
        .onChange(of: model.sessionExpired) { _, expired in
            guard expired else { return }
            Task { await auth.signOutAfterRejectedSession() }
        }
        .navigationDestination(item: $pickerKind) { kind in
            GroupPickerForProposalView(
                auth: auth,
                symbol: symbol,
                kind: kind,
                stock: proposeStock,
                onProposed: { cabalName in
                    pickerKind = nil
                    Haptics.success()
                    toast = MonacoToast(message: ProposeFlowCopy.proposalSent(cabalName), isSuccess: true)
                }
            )
        }
        .monacoFrameStats("AssetDetail")
    }

    /// Everything the propose flow needs, so it never refetches what this screen already showed.
    private var proposeStock: ProposeStock {
        guard let detail = model.detail else { return ProposeStock(symbol: symbol) }
        return ProposeStock(
            symbol: detail.symbol,
            name: ProposeStock.displayName(symbol: detail.symbol, catalogName: detail.name),
            priceMicros: detail.priceUsdcMicros,
            change24h: detail.change24h,
            isTradable: detail.liquidity.routable
        )
    }

    private func header(_ detail: AssetDetailDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoHeroHeader(
                title: formattedPrice(detail.priceUsdcMicros),
                // Same resolver as the list rows, so one stock never carries two names.
                caption: ProposeStock.displayName(symbol: detail.symbol, catalogName: detail.name)
            )
            if let move = model.move {
                HStack(spacing: MonacoTheme.Space.xs) {
                    Text(PercentReturnFormatter.format(move.ratio))
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.signed(move.ratio))
                    Text(move.label)
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("asset-detail-move")
            }
        }
    }

    private var headerSkeleton: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            SkeletonBlock(width: 120, height: 14)
            SkeletonBlock(width: 180, height: 34)
            SkeletonBlock(width: 100, height: 14)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityLabel("Loading stock")
    }

    @ViewBuilder
    private var chartSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(AssetChartRange.allCases, id: \.self) { range in
                    Button {
                        model.range = range
                    } label: {
                        MonacoChip(title: range.label, isSelected: model.range == range)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(range.accessibilityLabel)
                    .accessibilityIdentifier("asset-chart-range-\(range.rawValue)")
                }
            }

            switch model.chartState {
            case .loading:
                chartPlaceholder
                    .accessibilityLabel("Loading price history")
                    .accessibilityIdentifier("asset-detail-chart-loading")
            case .series(let points):
                chart(points)
            case .empty:
                EmptyState(title: "No price history for this window yet")
                    .accessibilityIdentifier("asset-detail-chart-empty")
            case .failed:
                EmptyState(
                    title: "Could not load price history",
                    actionTitle: "Retry",
                    action: { Task { await model.loadChart(range: model.range) } }
                )
                .accessibilityIdentifier("asset-detail-chart-failed")
            }
        }
    }

    private func chart(_ points: [AssetChartPointDTO]) -> some View {
        // The figure's own verdict, not a second one computed from the series: a move the header
        // prints as flat must not be drawn as a gain.
        let tint = curveTint
        // Prices live far from zero, so the area is clipped to the series' own range.
        let low = points.map(\.chartValue).min() ?? 0
        let high = points.map(\.chartValue).max() ?? 0
        let pad = max((high - low) * 0.12, 0.01)

        return Chart(points) { point in
            AreaMark(
                x: .value("Time", point.date),
                yStart: .value("Floor", low - pad),
                yEnd: .value("Price", point.chartValue)
            )
            .foregroundStyle(
                LinearGradient(
                    colors: [tint.opacity(0.26), tint.opacity(0)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            )
            // Monotone, not Catmull-Rom: sparse series must not draw peaks the data never had.
            .interpolationMethod(.monotone)
            LineMark(
                x: .value("Time", point.date),
                y: .value("Price", point.chartValue)
            )
            .foregroundStyle(tint)
            .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
            .interpolationMethod(.monotone)
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartYScale(domain: (low - pad)...(high + pad))
        .frame(height: 180)
        .accessibilityElement()
        .accessibilityLabel(chartAccessibilityLabel(points))
        .accessibilityIdentifier("asset-detail-chart")
    }

    /// Vivid counterpart of `MonacoTheme.signed(move.ratio)`, so the curve and the figure under
    /// the price always carry the same verdict.
    private var curveTint: Color {
        switch model.move?.direction {
        case .up: return MonacoTheme.profitVivid
        case .down: return MonacoTheme.lossVivid
        case .flat, nil: return MonacoTheme.muted
        }
    }

    private var chartPlaceholder: some View {
        SkeletonBlock(width: nil, height: 180, radius: MonacoTheme.Radius.card)
            .frame(maxWidth: .infinity)
    }

    private func chartAccessibilityLabel(_ points: [AssetChartPointDTO]) -> String {
        let low = points.map(\.chartValue).min() ?? 0
        let high = points.map(\.chartValue).max() ?? 0
        let move = model.move.map { PercentReturnFormatter.format($0.ratio) } ?? "—"
        return "\(model.range.accessibilityLabel) price history. "
            + "\(move) \(model.range.moveLabel.lowercased()). "
            + "Low \(UsdAmountFormatter.format(micros: Int64(low * 1_000_000))), "
            + "high \(UsdAmountFormatter.format(micros: Int64(high * 1_000_000)))."
    }

    private var actionRow: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Button("Buy") {
                pickerKind = .buy
            }
            .buttonStyle(.monacoPrimary)
            .disabled(!model.canBuy)
            .accessibilityIdentifier("asset-detail-buy")

            Button("Sell") {
                pickerKind = .sell
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("asset-detail-sell")
        }
    }

    private func formattedPrice(_ micros: Int64?) -> String {
        guard let micros else { return "—" }
        return UsdAmountFormatter.format(micros: micros)
    }
}
