import SwiftUI

/// Search catalog, fetch quote, propose buy when routable.
struct ProposeBuyView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()
    private let pageSize = 25
    private let searchDebounceNanos: UInt64 = 300_000_000
    /// xStock SPL tokens use 8 on-chain decimals (Jupiter outAmount atomics).
    private let xStockAtomicScale = Decimal(100_000_000)

    @State private var searchQuery = ""
    @State private var assets: [CatalogAssetDTO] = []
    @State private var hasMoreAssets = false
    @State private var catalogOffset = 0
    @State private var selectedSymbol: String?
    @State private var amountText = ""
    @State private var quote: BuyQuoteDTO?
    @State private var createdProposalId: String?
    @State private var errorMessage: String?
    @State private var isLoadingCatalog = false
    @State private var isLoadingMore = false
    @State private var catalogLoadFailed = false
    @State private var isLoadingTreasury = false
    @State private var treasuryLoadFailed = false
    @State private var treasuryUsdcMicros: Int64?
    @State private var isQuoting = false
    @State private var isProposing = false
    @State private var searchTask: Task<Void, Never>?

    var body: some View {
        Form {
            Section {
                Text("Buy Apple with your club — search a stock, check the quote, then propose.")
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
                } else if let treasuryUsdcMicros {
                    Text("Treasury available: \(formatUsd(microsToDecimal(String(treasuryUsdcMicros)) ?? 0))")
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
                Section("Amount (USDC) — \(selectedSymbol)") {
                    TextField("Amount", text: $amountText)
                        .keyboardType(.decimalPad)
                        .accessibilityIdentifier("proposal-amount-field")

                    if exceedsTreasury {
                        Label("Amount exceeds treasury USDC available.", systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(.orange)
                    }

                    Button(isQuoting ? "Checking quote…" : "Get quote") {
                        Task { await fetchQuote() }
                    }
                    .disabled(isQuoting || parsedUsdcMicro == nil || exceedsTreasury)
                    .accessibilityIdentifier("proposal-quote-button")
                }
            }

            if let quote {
                Section("Quote") {
                    Label(
                        quote.routable ? "Route available" : "No route right now",
                        systemImage: quote.routable ? "checkmark.seal" : "xmark.seal"
                    )
                    .foregroundStyle(quote.routable ? .green : .orange)

                    if quote.routable {
                        if let priceText = formattedPricePerShare(for: quote) {
                            LabeledContent("Price", value: priceText)
                        }
                        if let sharesText = formattedSharesReceived(for: quote) {
                            LabeledContent("Shares received", value: sharesText)
                        }
                    }

                    Button(isProposing ? "Proposing…" : "Propose buy") {
                        Task { await submitProposal() }
                    }
                    .disabled(isProposing || !quote.routable || exceedsTreasury)
                    .accessibilityIdentifier("proposal-submit-button")
                }
            }

            if let createdProposalId {
                Section {
                    Label("Proposal \(createdProposalId) created", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
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
        .navigationTitle("Propose buy")
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: searchQuery) { _, _ in
            scheduleCatalogSearch(reset: true)
        }
        .task {
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
                        Text(asset.name).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Buy") {
                        selectAsset(asset.symbol)
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .accessibilityIdentifier("proposal-buy-\(asset.symbol)")
                }
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
        guard let amount = parsedUsdcMicro, let treasury = treasuryUsdcMicros else {
            return false
        }
        return amount > treasury
    }

    private func selectAsset(_ symbol: String) {
        selectedSymbol = symbol
        quote = nil
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
            treasuryUsdcMicros = usdcMicrosFromPot(view.pot)
        } catch {
            treasuryLoadFailed = true
            treasuryUsdcMicros = nil
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
            let sorted = response.assets.sorted {
                $0.symbol.localizedCaseInsensitiveCompare($1.symbol) == .orderedAscending
            }
            if reset {
                assets = sorted
            } else {
                assets.append(contentsOf: sorted)
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

    private func fetchQuote() async {
        guard let token = auth.accessToken, let symbol = selectedSymbol, let usdc = parsedUsdcMicro else { return }
        if exceedsTreasury {
            errorMessage = "Amount exceeds treasury USDC available."
            return
        }
        isQuoting = true
        errorMessage = nil
        defer { isQuoting = false }

        do {
            quote = try await apiClient.postQuote(accessToken: token, groupId: groupId, symbol: symbol, usdc: usdc)
        } catch {
            errorMessage = "Could not fetch quote."
        }
    }

    private func submitProposal() async {
        guard let token = auth.accessToken, let quote, quote.routable,
              let usdc = parsedUsdcMicro else { return }
        if exceedsTreasury {
            errorMessage = "Amount exceeds treasury USDC available."
            return
        }
        isProposing = true
        errorMessage = nil
        defer { isProposing = false }

        do {
            let response = try await apiClient.createProposal(
                accessToken: token,
                groupId: groupId,
                symbol: quote.symbol,
                usdc: usdc
            )
            createdProposalId = response.proposalId
        } catch MonacoAPIError.httpStatus(400) {
            errorMessage = "Amount exceeds treasury USDC available."
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Proposal failed (HTTP \(code))."
        } catch {
            errorMessage = "Could not create proposal."
        }
    }

    private func usdcMicrosFromPot(_ pot: [PotRowDTO]) -> Int64? {
        guard let usdcRow = pot.first(where: { $0.symbol.uppercased() == "USDC" }),
              let decimal = Decimal(string: usdcRow.units, locale: Locale(identifier: "en_US_POSIX")) else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }

    private func formattedPricePerShare(for quote: BuyQuoteDTO) -> String? {
        if let priceMicros = quote.priceUsdcMicros, let value = microsToDecimal(priceMicros) {
            return formatUsd(value) + " / share"
        }
        guard let outputAmount = quote.outputAmount,
              let usdc = microsToDecimal(quote.usdcMicros),
              let shares = xStockAtomicsToShares(outputAmount),
              shares > 0 else {
            return nil
        }
        return formatUsd(usdc / shares) + " / share"
    }

    private func formattedSharesReceived(for quote: BuyQuoteDTO) -> String? {
        guard let outputAmount = quote.outputAmount, let shares = xStockAtomicsToShares(outputAmount) else {
            return nil
        }
        return formatShares(shares)
    }

    private func xStockAtomicsToShares(_ raw: String) -> Decimal? {
        guard let atomics = Decimal(string: raw, locale: Locale(identifier: "en_US_POSIX")) else {
            return nil
        }
        return atomics / xStockAtomicScale
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

    private func formatShares(_ value: Decimal) -> String {
        var rounded = Decimal()
        var source = value
        NSDecimalRound(&rounded, &source, 4, .plain)
        let number = rounded as NSDecimalNumber
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = 4
        formatter.locale = Locale(identifier: "en_US_POSIX")
        let formatted = formatter.string(from: number) ?? number.stringValue
        return "\(formatted) shares"
    }
}
