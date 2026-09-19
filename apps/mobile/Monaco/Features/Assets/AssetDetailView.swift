import Charts
import MonacoCore
import SwiftUI

struct AssetDetailView: View {
    @ObservedObject var auth: PrivyAuthService
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
                    if !detail.liquidity.routable {
                        Text("No route for this stock right now.")
                            .font(MonacoTheme.TypeRole.caption)
                            .foregroundStyle(MonacoTheme.warning)
                            .accessibilityIdentifier("asset-detail-no-route")
                    }
                    chartSection
                    liquidityCard(detail.liquidity)
                    actionRow
                }
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle(detail?.displayTicker ?? AssetSymbolFormatter.format(symbol))
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
                Chart(chart.points) { point in
                    LineMark(
                        x: .value("Time", point.date),
                        y: .value("Price", point.chartValue)
                    )
                    .foregroundStyle(MonacoTheme.accent)
                    .lineStyle(StrokeStyle(lineWidth: 2))
                }
                .chartXAxis(.hidden)
                .chartYAxis {
                    AxisMarks(position: .leading, values: .automatic(desiredCount: 3))
                }
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

    private func liquidityCard(_ liquidity: AssetLiquidityDTO) -> some View {
        MonacoCard {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Text(liquidity.label)
                    .font(MonacoTheme.TypeRole.title)
                    .foregroundStyle(MonacoTheme.ink)
                Text(liquidity.routable ? "Route available" : "No route for this stock right now.")
                    .font(MonacoTheme.TypeRole.body)
                    .foregroundStyle(MonacoTheme.muted)
                if let spread = liquidity.spreadBps {
                    Text("Spread \(spread) bps")
                        .font(MonacoTheme.TypeRole.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
            }
        }
        .accessibilityIdentifier("asset-detail-jupiter")
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
