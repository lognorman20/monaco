import SwiftUI

struct ProposeSellView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let holdings: [PotRowDTO]
    var initialSymbol: String? = nil

    private let apiClient = MonacoAPIClient()
    @Environment(\.dismiss) private var dismiss
    @State private var selected: PotRowDTO?
    @State private var amountText = ""
    @State private var quote: BuyQuoteDTO?
    @State private var errorMessage: String?
    @State private var isQuoting = false
    @State private var isSubmitting = false
    @State private var toast: MonacoToast?
    @State private var thesisText = ""

    var body: some View {
        Form {
            Section("Held stocks") {
                ForEach(holdings) { row in
                    Button {
                        selected = row
                        amountText = ""
                        quote = nil
                    } label: {
                        HStack {
                            Text(row.symbol)
                            Spacer()
                            Text(row.units)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityIdentifier("proposal-sell-\(row.symbol)")
                }
            }

            if let selected {
                Section("Amount") {
                    Text("You can sell up to \(selected.units) shares of \(selected.symbol).")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    TextField("Shares", text: $amountText)
                        .keyboardType(.decimalPad)
                        .accessibilityIdentifier("proposal-sell-amount")
                    Button(isQuoting ? "Checking…" : "Get quote") {
                        Task { await quoteSell() }
                    }
                    .disabled(isQuoting || tokenAmount == nil)
                    .accessibilityIdentifier("proposal-sell-quote")
                }

                Section("Thesis (optional)") {
                    Text("Why are you closing this position? Voters will see this.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    TextEditor(text: $thesisText)
                        .frame(minHeight: 80)
                        .accessibilityIdentifier("proposal-sell-thesis-field")
                }
            }

            if let quote, quote.routable {
                Section("Quote") {
                    Text("Estimated USDC \(quote.outputUsdcMicros ?? quote.outputAmount ?? "0")")
                    Button(isSubmitting ? "Proposing…" : "Submit proposal") {
                        Task { await submit() }
                    }
                    .disabled(isSubmitting)
                    .accessibilityIdentifier("proposal-sell-submit")
                }
            }

            if let errorMessage {
                Section {
                    Text(errorMessage).foregroundStyle(.red)
                }
            }
        }
        .navigationTitle("Propose sell")
        .monacoToast($toast)
        .task {
            if selected == nil, let initialSymbol {
                selected = holdings.first {
                    $0.symbol.caseInsensitiveCompare(initialSymbol) == .orderedSame
                }
            }
        }
    }

    private var tokenAmount: Int64? {
        guard let selected, let parsed = decimalSharesToAtomics(amountText) else { return nil }
        let ceiling = Int64(selected.tokenAmount ?? "0") ?? 0
        guard parsed >= 1, parsed <= ceiling else { return nil }
        return parsed
    }

    private func quoteSell() async {
        guard let token = auth.accessToken, let selected, let amount = tokenAmount else { return }
        isQuoting = true
        errorMessage = nil
        defer { isQuoting = false }
        do {
            quote = try await apiClient.postQuote(
                accessToken: token,
                groupId: groupId,
                symbol: selected.symbol,
                kind: "sell",
                usdc: nil,
                tokenAmount: amount
            )
            if quote?.routable != true {
                errorMessage = "No sell route for this amount. Try a larger amount."
            }
        } catch MonacoAPIError.httpStatus(400) {
            errorMessage = "That amount is no longer available to sell."
        } catch {
            errorMessage = "Could not get a sell quote."
        }
    }

    private func submit() async {
        guard let token = auth.accessToken, let selected, let amount = tokenAmount else { return }
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }
        let trimmedThesis = thesisText.trimmingCharacters(in: .whitespacesAndNewlines)

        do {
            _ = try await apiClient.createProposal(
                accessToken: token,
                groupId: groupId,
                kind: "sell",
                symbol: selected.symbol,
                tokenAmount: amount,
                thesis: trimmedThesis.isEmpty ? nil : trimmedThesis
            )
            toast = MonacoToast(message: "Proposal submitted", isSuccess: true)
            dismiss()
        } catch MonacoAPIError.apiError(_, let message) where message == "thesis exceeds maximum length" {
            errorMessage = "Thesis is too long. Keep it under 500 characters."
        } catch MonacoAPIError.httpStatus(400) {
            errorMessage = "That amount is no longer available to sell."
        } catch {
            errorMessage = "Could not submit proposal."
        }
    }
}

private func decimalSharesToAtomics(_ raw: String) -> Int64? {
    let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !trimmed.isEmpty else { return nil }
    guard var parsed = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")) else { return nil }
    let scale = Decimal(sign: .plus, exponent: 8, significand: 1)
    parsed *= scale
    var rounded = Decimal()
    NSDecimalRound(&rounded, &parsed, 0, .down)
    let value = NSDecimalNumber(decimal: rounded).int64Value
    return value > 0 ? value : nil
}
