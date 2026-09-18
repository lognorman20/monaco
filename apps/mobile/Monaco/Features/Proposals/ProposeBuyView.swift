import SwiftUI

/// Search catalog, enter amount, navigate to quote detail.
struct ProposeBuyView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    var initialSymbol: String? = nil

    private let apiClient = MonacoAPIClient()
    private let pageSize = 25
    private let searchDebounceNanos: UInt64 = 300_000_000

    @State private var searchQuery = ""
    @State private var assets: [CatalogAssetDTO] = []
    @State private var hasMoreAssets = false
    @State private var catalogOffset = 0
    @State private var selectedSymbol: String?
    @State private var amountText = ""
    @State private var thesis = ""
    @State private var errorMessage: String?
    @State private var isLoadingCatalog = false
    @State private var isLoadingMore = false
    @State private var catalogLoadFailed = false
    @State private var isLoadingTreasury = false
    @State private var treasuryLoadFailed = false
    @State private var treasuryTotalMicros: Int64?
    @State private var searchTask: Task<Void, Never>?

    var body: some View {
        Form {
            Section {
                Text("Buy Apple with your cabal — search a stock, check the quote, then propose.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Section("Search stocks") {
                TextField("e.g. AAPL", text: $searchQuery)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("proposal-search-field")

                if isLoadingTreasury {
                    HStack {
                        ProgressView()
                        Text("Loading treasury…")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } else if treasuryLoadFailed {
                    Label("Could not load treasury balance.", systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                    Button("Retry treasury") {
                        Task { await loadTreasury() }
                    }
                } else if let treasuryTotalMicros {
                    Text("Treasury total: \(formatUsd(microsToDecimal(String(treasuryTotalMicros)) ?? 0))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                catalogContent

                if hasMoreAssets && !isLoadingCatalog {
                    Button(isLoadingMore ? "Loading…" : "Load more") {
                        Task { await loadCatalog(reset: false) }
                    }
                    .disabled(isLoadingMore)
                    .accessibilityIdentifier("proposal-load-more")
                }
            }

            if let selectedSymbol {
                Section("Thesis") {
                    TextField("Why should the cabal buy this?", text: $thesis, axis: .vertical)
                        .lineLimit(3...8)
                        .accessibilityIdentifier("proposal-thesis-field")
                    Text("Optional · Up to 2,000 characters")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }

                Section("Amount (USDC) — \(selectedSymbol)") {
                    TextField("Amount", text: $amountText)
                        .keyboardType(.decimalPad)
                        .accessibilityIdentifier("proposal-amount-field")

                    if exceedsTreasury {
                        Label("Amount exceeds treasury total available.", systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(.orange)
                    }

                    if let usdcMicros = parsedUsdcMicro {
                        NavigationLink {
                            ProposeQuoteDetailView(
                                auth: auth,
                                groupId: groupId,
                                symbol: selectedSymbol,
                                usdcMicros: usdcMicros,
                                treasuryTotalMicros: treasuryTotalMicros,
                                thesis: thesis
                            )
                        } label: {
                            Text("Get quote")
                        }
                        .disabled(exceedsTreasury)
                        .accessibilityIdentifier("proposal-quote-button")
                    } else {
                        Button("Get quote") {}
                            .disabled(true)
                            .accessibilityIdentifier("proposal-quote-button")
                    }
                }
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Propose buy")
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: searchQuery) { _, _ in
            scheduleCatalogSearch(reset: true)
        }
        .task {
            if selectedSymbol == nil, let initialSymbol {
                selectedSymbol = initialSymbol
                if searchQuery.isEmpty {
                    searchQuery = initialSymbol
                }
            }
            await loadTreasury()
            await loadCatalog(reset: true)
        }
        .onDisappear {
            searchTask?.cancel()
        }
    }

    @ViewBuilder
    private var catalogContent: some View {
        if isLoadingCatalog && assets.isEmpty {
            HStack {
                ProgressView()
                Text("Loading stocks…")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } else if catalogLoadFailed && assets.isEmpty {
            Label("Could not load stocks.", systemImage: "exclamationmark.triangle.fill")
                .font(.footnote)
                .foregroundStyle(.orange)
            Button("Retry") {
                Task { await loadCatalog(reset: true) }
            }
        } else if assets.isEmpty {
            Text(searchQuery.isEmpty ? "Type to search stocks." : "No matches for \"\(searchQuery)\".")
                .font(.footnote)
                .foregroundStyle(.secondary)
        } else {
            ForEach(assets) { asset in
                HStack {
                    VStack(alignment: .leading) {
                        Text(asset.symbol).font(.body.bold())
                        Text(asset.displayName).font(.caption).foregroundStyle(.secondary)
                        if !asset.isTradable {
                            Text("No quote")
                                .font(.caption2)
                                .foregroundStyle(.tertiary)
                        }
                    }
                    Spacer()
                    Button("Buy") {
                        selectAsset(asset.symbol)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .accessibilityIdentifier("proposal-buy-\(asset.symbol)")
                }
                .opacity(asset.isTradable ? 1 : 0.55)
                .contentShape(Rectangle())
                .onTapGesture {
                    selectAsset(asset.symbol)
                }
                .accessibilityIdentifier("proposal-asset-\(asset.symbol)")
            }
        }
    }

    private var parsedUsdcMicro: Int64? {
        let trimmed = amountText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty,
              let decimal = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")),
              decimal > 0 else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        let micro = (rounded as NSDecimalNumber).int64Value
        return micro > 0 ? micro : nil
    }

    private var exceedsTreasury: Bool {
        guard let amount = parsedUsdcMicro, let treasury = treasuryTotalMicros else {
            return false
        }
        return amount > treasury
    }

    private func selectAsset(_ symbol: String) {
        selectedSymbol = symbol
        errorMessage = nil
    }

    private func scheduleCatalogSearch(reset: Bool) {
        searchTask?.cancel()
        searchTask = Task {
            do {
                try await Task.sleep(nanoseconds: searchDebounceNanos)
            } catch {
                return
            }
            guard !Task.isCancelled else { return }
            await loadCatalog(reset: reset)
        }
    }

    private func loadTreasury() async {
        guard let token = auth.accessToken else { return }
        isLoadingTreasury = true
        treasuryLoadFailed = false
        defer { isLoadingTreasury = false }

        do {
            let view = try await apiClient.getGroupView(accessToken: token, groupId: groupId)
            treasuryTotalMicros = usdcMicrosFromUsdDecimal(view.resolvedPotTotalUsd)
        } catch {
            treasuryLoadFailed = true
            treasuryTotalMicros = nil
        }
    }

    private func loadCatalog(reset: Bool) async {
        guard let token = auth.accessToken else { return }
        if reset {
            isLoadingCatalog = true
            catalogLoadFailed = false
            catalogOffset = 0
            hasMoreAssets = false
            if !assets.isEmpty {
                assets = []
            }
        } else {
            isLoadingMore = true
        }
        defer {
            isLoadingCatalog = false
            isLoadingMore = false
        }

        let query = searchQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        let offset = reset ? 0 : catalogOffset

        do {
            let response = try await apiClient.searchAssets(
                accessToken: token,
                groupId: groupId,
                query: query,
                limit: pageSize,
                offset: offset
            )
            if reset {
                assets = response.assets
            } else {
                assets.append(contentsOf: response.assets)
            }
            catalogOffset = assets.count
            hasMoreAssets = response.hasMore
        } catch {
            if reset {
                catalogLoadFailed = true
                assets = []
            } else {
                errorMessage = "Could not load more stocks."
            }
        }
    }

    private func usdcMicrosFromUsdDecimal(_ raw: String) -> Int64? {
        guard let decimal = Decimal(string: raw, locale: Locale(identifier: "en_US_POSIX")) else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        let micros = (rounded as NSDecimalNumber).int64Value
        return micros >= 0 ? micros : nil
    }

    private func microsToDecimal(_ raw: String) -> Decimal? {
        guard let micros = Decimal(string: raw, locale: Locale(identifier: "en_US_POSIX")) else {
            return nil
        }
        return micros / Decimal(1_000_000)
    }

    private func formatUsd(_ value: Decimal) -> String {
        var rounded = Decimal()
        var source = value
        NSDecimalRound(&rounded, &source, 2, .plain)
        let number = rounded as NSDecimalNumber
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: number) ?? "$\(number)"
    }
}
