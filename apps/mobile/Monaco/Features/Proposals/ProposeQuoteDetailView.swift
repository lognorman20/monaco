import SwiftUI

/// Quote + propose for one buy — loaded after Get quote from amount screen.
struct ProposeQuoteDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let symbol: String
    let usdcMicros: Int64
    let treasuryUsdcMicros: Int64?

    private let apiClient = MonacoAPIClient()
    /// xStock SPL tokens use 8 on-chain decimals (Jupiter outAmount atomics).
    private let xStockAtomicScale = Decimal(100_000_000)

    @State private var quote: BuyQuoteDTO?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var isProposing = false
    @State private var createdProposalId: String?

    var body: some View {
        Form {
            Section {
                LabeledContent("Symbol", value: symbol)
                LabeledContent("Amount", value: formatUsd(microsToDecimal(String(usdcMicros)) ?? 0))
            }

            if isLoading {
                Section {
                    ProgressView("Checking quote…")
                        .tint(MonacoTheme.accent)
                }
            } else if let quote {
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

                    if exceedsTreasury {
                        Label("Amount exceeds treasury USDC available.", systemImage: "exclamationmark.triangle.fill")
                            .font(.footnote)
                            .foregroundStyle(.orange)
                    }

                    Button(isProposing ? "Proposing…" : "Propose buy") {
                        Task { await submitProposal() }
                    }
                    .disabled(isProposing || !quote.routable || exceedsTreasury)
                    .accessibilityIdentifier("proposal-submit-button")
                }
            } else if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                    Button("Try again") {
                        Task { await fetchQuote() }
                    }
                    .monacoFormSecondaryAction()
                }
            }

            if let createdProposalId {
                Section {
                    Label("Proposal \(createdProposalId) created", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Quote")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: loadTaskID) {
            await fetchQuote()
        }
    }

    private var loadTaskID: String {
        "\(groupId)-\(symbol)-\(usdcMicros)-\(auth.accessToken ?? "")"
    }

    private var exceedsTreasury: Bool {
        guard let treasury = treasuryUsdcMicros else { return false }
        return usdcMicros > treasury
    }

    private func fetchQuote() async {
        guard let token = auth.accessToken else { return }
        isLoading = true
        errorMessage = nil
        quote = nil
        defer { isLoading = false }

        do {
            quote = try await apiClient.postQuote(
                accessToken: token,
                groupId: groupId,
                symbol: symbol,
                usdc: usdcMicros
            )
        } catch {
            errorMessage = "Could not fetch quote."
        }
    }

    private func submitProposal() async {
        guard let token = auth.accessToken, let quote, quote.routable else { return }
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
                usdc: usdcMicros
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
