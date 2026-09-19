import SwiftUI

/// Quote + propose for one buy — loaded after Get quote from amount screen.
struct ProposeQuoteDetailView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let symbol: String
    let usdcMicros: Int64
    let treasuryTotalMicros: Int64?

    private let apiClient = MonacoAPIClient()
    /// xStock SPL tokens use 8 on-chain decimals (Jupiter outAmount atomics).
    private let xStockAtomicScale = Decimal(100_000_000)

    @State private var quote: BuyQuoteDTO?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var isProposing = false
    @State private var toast: MonacoToast?

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
                        Label("Amount exceeds treasury total available.", systemImage: "exclamationmark.triangle.fill")
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

        }
        .monacoFormScreen()
        .monacoToast($toast)
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
        guard let treasury = treasuryTotalMicros else { return false }
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
                kind: "buy",
                usdc: usdcMicros
            )
        } catch {
            errorMessage = "Could not fetch quote."
        }
    }

    private func submitProposal() async {
        guard let token = auth.accessToken, let quote, quote.routable else { return }
        if exceedsTreasury {
            return
        }
        isProposing = true
        defer { isProposing = false }

        do {
            let response = try await apiClient.createProposal(
                accessToken: token,
                groupId: groupId,
                kind: "buy",
                symbol: quote.symbol,
                usdcMicros: usdcMicros
            )
            toast = MonacoToast(message: proposalSubmittedMessage(id: response.proposalId), isSuccess: true)
        } catch MonacoAPIError.apiError(_, let message) where message == "amount exceeds treasury total available" {
            toast = MonacoToast(message: "Amount exceeds treasury total available.")
        } catch MonacoAPIError.apiError(_, let message) where message == "quote not routable" {
            toast = MonacoToast(message: "No route available right now.")
        } catch MonacoAPIError.httpStatus(let code) {
            toast = MonacoToast(message: "Proposal failed (HTTP \(code)).")
        } catch {
            toast = MonacoToast(message: "Could not create proposal.")
        }
    }

    private func proposalSubmittedMessage(id: String) -> String {
        let truncated = id.count > 8 ? String(id.prefix(8)) + "…" : id
        return "Proposal submitted (\(truncated))"
    }

    private func formattedPricePerShare(for quote: BuyQuoteDTO) -> String? {
        if let priceMicros = quote.priceUsdcMicros, let value = microsToDecimal(priceMicros) {
            return formatUsd(value) + " / share"
        }
        guard let outputAmount = quote.outputAmount,
              let usdcRaw = quote.usdcMicros,
              let usdc = microsToDecimal(usdcRaw),
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
