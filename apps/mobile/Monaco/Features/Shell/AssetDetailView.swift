import MonacoCore
import Charts
import SwiftUI

/// Per-stock detail: identity, real price history, trade availability, and buy entry.
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
            VStack(alignment: .leading, spacing: 24) {
                if isLoading && detail == nil {
                    ProgressView("Loading stock…")
                        .frame(maxWidth: .infinity, alignment: .center)
                        .padding(.top, 40)
                } else if let detail {
                    headerSection(detail)
                    chartSection
                    liquiditySection(detail.liquidity)
                    buySection(detail)
                } else if let errorMessage {
                    MonacoEmptyStateCard(message: errorMessage, systemImage: "exclamationmark.triangle")
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 20)
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

    private func headerSection(_ detail: AssetDetailDTO) -> some View {
        VStack(alignment: .leading, spacing: 24) {
            HStack(spacing: 14) {
                MonacoIdentityMark(title: detail.displaySymbol, size: 60)
                VStack(alignment: .leading, spacing: 4) {
                    Text(detail.displaySymbol)
                        .font(MonacoTheme.display(26))
                    Text(detail.displayName)
                        .font(.subheadline)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
            }
            VStack(alignment: .leading, spacing: 12) {
                Text(MarketFormatters.usd(fromMicros: detail.priceUsdcMicros))
                    .font(MonacoTheme.display(44).monospacedDigit())
                    .minimumScaleFactor(0.65)
                    .lineLimit(1)
                if let change = MarketFormatters.percentChange(detail.change24h) {
                    Text("\(change) past 24h")
                        .font(.subheadline.weight(.semibold).monospacedDigit())
                        .foregroundStyle(change.hasPrefix("-") ? MonacoTheme.warning : MonacoTheme.success)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                        .background(change.hasPrefix("-") ? MonacoTheme.peach : MonacoTheme.mint, in: Capsule())
                }
            }
        }
        .foregroundStyle(MonacoTheme.primaryText)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var chartSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Price history")
                .font(MonacoTheme.display(21))
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
                    .foregroundStyle(MonacoTheme.primaryText)
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
        .padding(20)
        .monacoSurfaceCard()
    }

    private func liquiditySection(_ liquidity: AssetLiquidityDTO) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Trade availability")
                .font(MonacoTheme.display(21))
                .foregroundStyle(MonacoTheme.primaryText)
            Label(liquidity.routable ? "Available to propose" : "Not available to buy right now",
                  systemImage: liquidity.routable ? "checkmark.circle.fill" : "clock")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(liquidity.routable ? MonacoTheme.primaryText : MonacoTheme.secondaryText)
            if let spread = liquidity.spreadBps {
                LabeledContent("Spread vs. market price", value: "\(spread) bps")
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.secondaryText)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(MonacoTheme.mint, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
    }

    private func buySection(_ detail: AssetDetailDTO) -> some View {
        NavigationLink {
            GroupPickerForProposalView(auth: auth, home: home, symbol: symbol)
        } label: {
            Text(detail.routable ? "Buy" : "Unavailable to buy")
                .font(.headline)
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.monacoPrimary)
        .disabled(!detail.routable)
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
