import SwiftUI

/// Search catalog, fetch quote, propose buy when routable.
struct ProposeBuyView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var searchQuery = ""
    @State private var assets: [CatalogAssetDTO] = []
    @State private var selectedSymbol: String?
    @State private var amountText = ""
    @State private var quote: BuyQuoteDTO?
    @State private var createdProposalId: String?
    @State private var errorMessage: String?
    @State private var isSearching = false
    @State private var isQuoting = false
    @State private var isProposing = false

    var body: some View {
        Form {
            Section {
                Text("Buy Apple with your club — search a stock, check the quote, then propose.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Section("Search stocks") {
                HStack {
                    TextField("e.g. AAPL", text: $searchQuery)
                        .textInputAutocapitalization(.characters)
                        .autocorrectionDisabled()
                        .accessibilityIdentifier("proposal-search-field")
                    Button(isSearching ? "…" : "Search") {
                        Task { await searchAssets() }
                    }
                    .disabled(isSearching || searchQuery.trimmingCharacters(in: .whitespaces).isEmpty)
                    .accessibilityIdentifier("proposal-search-button")
                }

                ForEach(assets) { asset in
                    Button {
                        selectedSymbol = asset.symbol
                        quote = nil
                    } label: {
                        HStack {
                            VStack(alignment: .leading) {
                                Text(asset.symbol).font(.body.bold())
                                Text(asset.name).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if selectedSymbol == asset.symbol {
                                Image(systemName: "checkmark.circle.fill")
                            }
                        }
                    }
                    .accessibilityIdentifier("proposal-asset-\(asset.symbol)")
                }
            }

            if selectedSymbol != nil {
                Section("Amount (USDC)") {
                    TextField("Amount", text: $amountText)
                        .keyboardType(.decimalPad)
                        .accessibilityIdentifier("proposal-amount-field")

                    Button(isQuoting ? "Checking quote…" : "Get quote") {
                        Task { await fetchQuote() }
                    }
                    .disabled(isQuoting || parsedUsdcMicro == nil)
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

                    Button(isProposing ? "Proposing…" : "Propose buy") {
                        Task { await submitProposal() }
                    }
                    .disabled(isProposing || !quote.routable)
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

    private func searchAssets() async {
        guard let token = auth.accessToken else { return }
        isSearching = true
        errorMessage = nil
        defer { isSearching = false }

        do {
            let response = try await apiClient.searchAssets(
                accessToken: token,
                groupId: groupId,
                query: searchQuery.trimmingCharacters(in: .whitespaces)
            )
            assets = response.assets
        } catch {
            errorMessage = "Could not search stocks."
        }
    }

    private func fetchQuote() async {
        guard let token = auth.accessToken, let symbol = selectedSymbol, let usdc = parsedUsdcMicro else { return }
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
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Proposal failed (HTTP \(code))."
        } catch {
            errorMessage = "Could not create proposal."
        }
    }
}
