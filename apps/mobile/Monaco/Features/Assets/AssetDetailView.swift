import Charts
import MonacoCore
import SwiftUI

struct AssetDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let symbol: String

    private let apiClient = MonacoAPIClient()

    @State private var activeSymbol: String
    @State private var detail: AssetDetailDTO?
    @State private var chart: AssetChartDTO?
    @State private var chartRange: AssetChartRange = .oneDay
    @State private var isLoading = true
    @State private var loadFailed = false
    @State private var pickerKind: ProposalPickKind?
    @State private var aboutExpanded = false

    init(auth: PrivyAuthService, symbol: String) {
        self.auth = auth
        self.symbol = symbol
        _activeSymbol = State(initialValue: symbol)
    }

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
                    if detail.resolvedKind == .preIpo {
                        preIpoMeta(detail)
                        referenceRow(detail)
                        aboutPreIpoCard
                        variantsSection(detail)
                    }
                    if !detail.liquidity.routable {
                        Text("Can't be bought right now.")
                            .font(MonacoTheme.TypeRole.caption)
                            .foregroundStyle(MonacoTheme.warning)
                            .accessibilityIdentifier("asset-detail-no-route")
                    }
                    chartSection(detail)
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
        .task(id: activeSymbol) {
            await loadDetail()
            await loadChart()
        }
        .onChange(of: chartRange) { _, _ in
            Task { await loadChart() }
        }
        .navigationDestination(item: $pickerKind) { kind in
            GroupPickerForProposalView(
                auth: auth,
                symbol: activeSymbol,
                assetKind: detail?.resolvedKind ?? .stock,
                tokenDecimals: detail?.resolvedDecimals ?? AssetCatalogDefaults.decimals,
                kind: kind
            )
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
    private func preIpoMeta(_ detail: AssetDetailDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
            if let sector = detail.sector, !sector.isEmpty {
                Text(sector)
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.muted)
            }
            Text(PreIpoCopy.tradesAroundTheClock)
                .font(MonacoTheme.TypeRole.caption)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    @ViewBuilder
    private func referenceRow(_ detail: AssetDetailDTO) -> some View {
        MonacoGroupedList {
            MonacoRow(
                title: PreIpoCopy.privateMarketReference,
                isLast: true
            ) {
                EmptyView()
            } trailing: {
                VStack(alignment: .trailing, spacing: 4) {
                    if let mark = detail.referenceMarkUsdcMicros, detail.premiumBps != nil {
                        Text(UsdAmountFormatter.format(micros: mark))
                            .font(MonacoTheme.Typo.moneyRow)
                    } else {
                        Text(PreIpoCopy.referenceUnavailable)
                            .font(MonacoTheme.Typo.body)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                    if let bps = detail.premiumBps {
                        Text(PreIpoCopy.premiumChip(bps: bps))
                            .font(MonacoTheme.Typo.caption.weight(.semibold))
                            .foregroundStyle(abs(bps) >= 1000 ? MonacoTheme.warning : MonacoTheme.muted)
                    }
                    if let caption = companyValueCaption(detail) {
                        Text(caption)
                            .font(MonacoTheme.Typo.micro)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                            .multilineTextAlignment(.trailing)
                    }
                }
            }
        }
    }

    private var aboutPreIpoCard: some View {
        DisclosureGroup(isExpanded: $aboutExpanded) {
            Text(PreIpoCopy.disclosure)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .padding(.top, MonacoTheme.Space.xs)
            Link(destination: URL(string: PreIpoCopy.termsURL)!) {
                MonacoRow(title: PreIpoCopy.termsLinkTitle, chevron: true, isLast: true) {
                    EmptyView()
                } trailing: { EmptyView() }
            }
            .buttonStyle(.plain)
        } label: {
            Text(PreIpoCopy.aboutCardTitle)
                .font(MonacoTheme.Typo.section)
                .foregroundStyle(MonacoTheme.ink)
        }
        .padding(MonacoTheme.Space.m)
        .background(MonacoTheme.surface)
        .clipShape(RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
    }

    @ViewBuilder
    private func variantsSection(_ detail: AssetDetailDTO) -> some View {
        let variants = detail.variants ?? []
        if variants.count > 1 {
            MonacoSectionHeader(PreIpoCopy.alsoAvailableFrom)
            MonacoGroupedList {
                ForEach(Array(variants.enumerated()), id: \.element.id) { index, variant in
                    Button {
                        activeSymbol = variant.symbol
                    } label: {
                        MonacoRow(
                            title: variant.issuer.capitalized,
                            subtitle: variantLiquidity(variant),
                            chevron: variant.symbol != activeSymbol,
                            isLast: index == variants.count - 1
                        ) {
                            EmptyView()
                        } trailing: {
                            if let micros = variant.priceUsdcMicros {
                                MoneyText(micros: micros, style: .row)
                            }
                        }
                    }
                    .buttonStyle(.monacoRow)
                }
            }
        }
    }

    @ViewBuilder
    private func chartSection(_ detail: AssetDetailDTO) -> some View {
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
                let rises = (chart.points.last?.chartValue ?? 0) >= (chart.points.first?.chartValue ?? 0)
                let tint = rises ? MonacoTheme.profitVivid : MonacoTheme.lossVivid
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
                    message: detail.resolvedKind == .preIpo ? PreIpoCopy.chartEmpty : "Price history is not available yet.",
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

    private func companyValueCaption(_ detail: AssetDetailDTO) -> String? {
        guard let valuation = detail.referenceValuationUsd else { return nil }
        let value = UsdAmountFormatter.compact(decimalString: String(valuation))
        guard let updated = detail.referenceUpdatedAt, let age = RelativeTimeFormatter.label(iso: updated), !age.isEmpty else {
            return "\(PreIpoCopy.companyValueCaption) \(value)"
        }
        return "\(PreIpoCopy.companyValueCaption) \(value) · updated \(age) ago"
    }

    private func variantLiquidity(_ variant: AssetVariantDTO) -> String {
        if let liquidity = variant.liquidityUsd, !liquidity.isEmpty {
            return "Liquidity \(UsdAmountFormatter.compact(decimalString: liquidity))"
        }
        return variant.symbol
    }

    private func loadDetail() async {
        guard let token = auth.accessToken else { return }
        isLoading = true
        loadFailed = false
        defer { isLoading = false }
        do {
            detail = try await apiClient.getMarketAsset(accessToken: token, symbol: activeSymbol)
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
                symbol: activeSymbol,
                range: chartRange
            )
        } catch {
            if error.isRequestCancellation { return }
            chart = AssetChartDTO(points: [], emptyReason: "price history unavailable")
        }
    }
}
