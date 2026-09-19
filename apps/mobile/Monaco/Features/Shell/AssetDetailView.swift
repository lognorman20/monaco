import MonacoCore
import Charts
import SwiftUI

/// Per-stock detail: price, chart, Jupiter liquidity snippet, and buy entry.
struct AssetDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    @Environment(\.refreshMonacoSession) private var refreshSession
    let symbol: String

    private let apiClient = MonacoAPIClient()

    @State private var detail: AssetDetailDTO?
    @State private var chart: AssetChartResponse?
    @State private var chartRange: AssetChartRange = .oneDay
    @State private var isLoading = true
    @State private var isLoadingChart = false
    @State private var errorMessage: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if isLoading && detail == nil {
                    ProgressView("Loading stock…")
                        .frame(maxWidth: .infinity, alignment: .center)
                        .padding(.top, 40)
                } else if let detail {
                    headerSection(detail)
                    chartSection
                    liquiditySection(detail.liquidity)
                    buySection
                } else if let errorMessage {
                    MonacoEmptyStateCard(message: errorMessage, systemImage: "exclamationmark.triangle")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
        }
        .background(MonacoTheme.background)
        .navigationTitle(detail?.displaySymbol ?? symbol)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await refreshSession()
            await loadDetail()
            await loadChart()
        }
        .task(id: symbol) {
            await loadDetail()
            await loadChart()
        }
        .onChange(of: chartRange) { _, _ in
            Task { await loadChart() }
        }
    }

    @ViewBuilder
    private func headerSection(_ detail: AssetDetailDTO) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(detail.displayName)
                .font(.title3.bold())
                .foregroundStyle(MonacoTheme.primaryText)
            Text(MarketFormatters.usd(fromMicros: detail.priceUsdcMicros))
                .font(.title.bold().monospacedDigit())
                .foregroundStyle(MonacoTheme.primaryText)
            if let change = MarketFormatters.percentChange(detail.change24h) {
                Text("\(change) past 24h")
                    .font(.subheadline)
                    .foregroundStyle(change.hasPrefix("-") ? MonacoTheme.warning : MonacoTheme.success)
            }
            if !detail.routable {
                Label("Not available to buy right now", systemImage: "exclamationmark.triangle.fill")
                    .font(.caption)
                    .foregroundStyle(MonacoTheme.warning)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var chartSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Picker("Range", selection: $chartRange) {
                ForEach(AssetChartRange.allCases) { range in
                    Text(range.title).tag(range)
                }
            }
            .pickerStyle(.segmented)

            if isLoadingChart {
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 180)
            } else if let chart, !chart.points.isEmpty {
                Chart(chart.points) { point in
                    LineMark(
                        x: .value("Time", Date(timeIntervalSince1970: TimeInterval(point.timestamp))),
                        y: .value("Price", Double(point.priceUsdcMicros) / 1_000_000)
                    )
                    .foregroundStyle(MonacoTheme.accent)
                }
                .chartYAxis {
                    AxisMarks(position: .leading)
                }
                .frame(height: 200)
            } else {
                MonacoEmptyStateCard(
                    message: chart?.emptyReason ?? "Price history unavailable.",
                    systemImage: "chart.xyaxis.line"
                )
            }
        }
        .padding(16)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    @ViewBuilder
    private func liquiditySection(_ liquidity: AssetLiquidityDTO) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(liquidity.label)
                .font(.headline)
                .foregroundStyle(MonacoTheme.primaryText)
            if liquidity.routable {
                if let outAmount = liquidity.buyProbeOutAmount {
                    Text("$1 USDC probe → \(outAmount) token atomics out")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                if let sellOut = liquidity.sellProbeOutAmount {
                    Text("1 share sell probe → \(sellOut) USDC atomics out")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                if let spread = liquidity.spreadBps {
                    Text("Spread vs mark: \(spread) bps")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            } else {
                Text("Not available to buy")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.warning)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    @ViewBuilder
    private var buySection: some View {
        NavigationLink {
            GroupPickerForProposalView(auth: auth, home: home, symbol: symbol)
        } label: {
            Text("Buy")
                .font(.headline)
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(MonacoTheme.accent)
        .accessibilityIdentifier("asset-detail-buy")
    }

    private func loadDetail() async {
        guard let token = auth.accessToken else { return }
        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            detail = try await apiClient.getMarketAsset(accessToken: token, symbol: symbol)
        } catch {
            errorMessage = "Could not load stock detail."
            detail = nil
        }
    }

    private func loadChart() async {
        guard let token = auth.accessToken else { return }
        isLoadingChart = true
        defer { isLoadingChart = false }

        do {
            chart = try await apiClient.getMarketAssetChart(
                accessToken: token,
                symbol: symbol,
                range: chartRange
            )
        } catch {
            chart = AssetChartResponse(points: [], emptyReason: "Price history unavailable.")
        }
    }
}
