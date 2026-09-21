import Charts
import MonacoCore
import SwiftUI

struct AssetDetailView: View {
    @ObservedObject var auth: DynamicAuthService
    let symbol: String

    private let apiClient = MonacoAPIClient()

    @State private var detail: AssetDetailDTO?
    @State private var chart: AssetChartDTO?
    @State private var chartRange: AssetChartRange = .oneDay
    @State private var isLoading = true
    @State private var loadFailed = false
    @State private var pickerKind: ProposalPickKind?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                if isLoading, detail == nil {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                } else if loadFailed, detail == nil {
                    MonacoEmptyStateCard(
                        message: "Could not load this stock.",
                        systemImage: "exclamationmark.triangle"
                    )
                    Button("Retry") {
                        Task { await loadDetail() }
                    }
                    .buttonStyle(.monacoSecondary)
                } else if let detail {
                    header(detail)
                    if !detail.routable {
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
        .navigationTitle(detail?.displayTicker ?? AssetSymbolFormatter.display(symbol))
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("asset-detail-root")
        .task {
            await loadDetail()
            await loadChart()
        }
        .onChange(of: chartRange) { _, _ in
            Task { await loadChart() }
        }
        .navigationDestination(item: $pickerKind) { kind in
            GroupPickerForProposalView(auth: auth, symbol: symbol, kind: kind)
        }
        .monacoFrameStats("AssetDetail")
    }

    private func header(_ detail: AssetDetailDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoHeroHeader(
                title: formattedPrice(detail.priceUsdcMicros),
                caption: detail.displayName
            )
            if let change = detail.change24h {
                Text(PercentReturnFormatter.format(change))
                    .font(MonacoTheme.TypeRole.body)
                    .foregroundStyle(MonacoTheme.signed(change))
            }
        }
    }

    @ViewBuilder
    private var chartSection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(spacing: MonacoTheme.Space.s) {
                ForEach(AssetChartRange.allCases, id: \.self) { range in
                    Button {
                        chartRange = range
                    } label: {
                        MonacoChip(title: range.label, isSelected: chartRange == range)
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("asset-chart-range-\(range.rawValue)")
                }
            }

            if let chart, chart.points.count >= 2 {
                // Gain/loss over the drawn window, so the curve agrees with the figure above it.
                let rises = (chart.points.last?.chartValue ?? 0) >= (chart.points.first?.chartValue ?? 0)
                let tint = rises ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
                // Prices live far from zero, so the area is clipped to the series' own range.
                let low = chart.points.map(\.chartValue).min() ?? 0
                let high = chart.points.map(\.chartValue).max() ?? 0
                let pad = max((high - low) * 0.12, 0.01)

                Chart(chart.points) { point in
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
                    .interpolationMethod(.catmullRom)
                    LineMark(
                        x: .value("Time", point.date),
                        y: .value("Price", point.chartValue)
                    )
                    .foregroundStyle(tint)
                    .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round))
                    .interpolationMethod(.catmullRom)
                }
                .chartXAxis(.hidden)
                .chartYAxis(.hidden)
                .chartYScale(domain: (low - pad)...(high + pad))
                .frame(height: 180)
                .accessibilityIdentifier("asset-detail-chart")
            } else {
                MonacoEmptyStateCard(
                    message: "Price history is not available yet.",
                    systemImage: "chart.line.uptrend.xyaxis"
                )
            }
        }
    }

    private var actionRow: some View {
        HStack(spacing: MonacoTheme.Space.s) {
            Button("Buy") {
                pickerKind = .buy
            }
            .buttonStyle(.monacoPrimary)
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

    private func loadDetail() async {
        guard let token = auth.accessToken else { return }
        isLoading = true
        loadFailed = false
        defer { isLoading = false }
        do {
            detail = try await apiClient.getMarketAsset(accessToken: token, symbol: symbol)
        } catch {
            if error.isRequestCancellation { return }
            loadFailed = true
        }
    }

    private func loadChart() async {
        guard let token = auth.accessToken else { return }
        do {
            chart = try await apiClient.getMarketAssetChart(
                accessToken: token,
                symbol: symbol,
                range: chartRange
            )
        } catch {
            if error.isRequestCancellation { return }
            chart = AssetChartDTO(points: [], emptyReason: "price history unavailable")
        }
    }
}
