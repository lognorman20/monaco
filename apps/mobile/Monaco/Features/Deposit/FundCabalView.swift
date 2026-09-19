import SwiftUI

/// Move USDC from account balance into a joined cabal treasury.
struct FundCabalView: View {
    @ObservedObject var auth: PrivyAuthService
    let joinedCabals: [HomeGroupBoardRowDTO]
    var preselectedGroupId: String?
    var onFunded: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @State private var balance: PlatformBalanceDTO?
    @State private var selectedGroupId: String?
    @State private var amountText = ""
    @State private var isLoadingBalance = true
    @State private var isSubmitting = false
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?

    var body: some View {
        Form {
            Section {
                if isLoadingBalance {
                    HStack(spacing: 12) {
                        ProgressView().tint(MonacoTheme.accent)
                        Text("Loading account balance…")
                            .monacoSecondaryCaption()
                    }
                } else if let balance {
                    LabeledContent("Available", value: formatUsdc(balance.availableUsdcMicros))
                        .accessibilityIdentifier("fund-cabal-available-balance")
                    if balance.pendingAllocationMicros > 0 {
                        Text("$\(formatUsdAmount(balance.pendingAllocationMicros)) is moving into a cabal.")
                            .monacoSecondaryCaption()
                    }
                } else {
                    Text(errorMessage ?? "Could not load account balance.")
                        .foregroundStyle(MonacoTheme.warning)
                }
            } header: {
                Text("Account balance")
            }

            Section("Choose cabal") {
                if joinedCabals.isEmpty {
                    Text("Join a cabal first, then fund it from your account balance.")
                        .monacoSecondaryCaption()
                } else {
                    Picker("Cabal", selection: $selectedGroupId) {
                        ForEach(joinedCabals) { cabal in
                            Text(cabal.name).tag(Optional(cabal.groupId))
                        }
                    }
                    .accessibilityIdentifier("fund-cabal-picker")
                }
            }

            Section("Amount") {
                TextField("USDC amount", text: $amountText)
                    .keyboardType(.decimalPad)
                    .accessibilityIdentifier("fund-cabal-amount-field")
                if let maxMicros = balance?.availableUsdcMicros, maxMicros > 0 {
                    Button("Use max (\(formatUsdc(maxMicros)))") {
                        amountText = String(format: "%.2f", Double(maxMicros) / 1_000_000.0)
                    }
                    .monacoFormSecondaryAction()
                    .accessibilityIdentifier("fund-cabal-max-button")
                }
            }

            Section {
                Button(isSubmitting ? "Funding…" : "Fund cabal") {
                    Task { await submitFund() }
                }
                .monacoFormPrimaryAction()
                .disabled(isSubmitting || joinedCabals.isEmpty || selectedGroupId == nil)
                .accessibilityIdentifier("fund-cabal-submit-button")
            }
        }
        .monacoFormScreen()
        .navigationTitle("Fund a cabal")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .task(id: auth.accessToken) {
            await loadBalance()
            if selectedGroupId == nil {
                selectedGroupId = preselectedGroupId ?? joinedCabals.first?.groupId
            }
        }
    }

    private func loadBalance() async {
        guard let token = auth.accessToken else {
            balance = nil
            errorMessage = "Sign in to view your account balance."
            isLoadingBalance = false
            return
        }

        isLoadingBalance = true
        errorMessage = nil
        do {
            balance = try await apiClient.getPlatformBalance(accessToken: token)
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not load balance (HTTP \(status))."
            balance = nil
        } catch {
            errorMessage = "Could not load account balance."
            balance = nil
        }
        isLoadingBalance = false
    }

    private func submitFund() async {
        guard let token = auth.accessToken else { return }
        guard let groupId = selectedGroupId else { return }
        guard let micros = parseUsdcMicros(amountText), micros > 0 else {
            toast = MonacoToast(message: "Enter a valid USDC amount.", isSuccess: false)
            return
        }
        if let available = balance?.availableUsdcMicros, micros > available {
            toast = MonacoToast(message: "Amount exceeds your available balance.", isSuccess: false)
            return
        }

        isSubmitting = true
        defer { isSubmitting = false }

        do {
            _ = try await apiClient.fundGroup(accessToken: token, groupId: groupId, amount: micros)
            toast = MonacoToast(message: "Funding started — your share updates when the transfer confirms.", isSuccess: true)
            amountText = ""
            await loadBalance()
            await onFunded()
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(message: "Amount exceeds your available balance.", isSuccess: false)
        } catch MonacoAPIError.httpStatus(403) {
            toast = MonacoToast(message: "You must be a cabal member to fund it.", isSuccess: false)
        } catch MonacoAPIError.httpStatus(let status) {
            toast = MonacoToast(message: "Could not fund cabal (HTTP \(status)).", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "Could not fund cabal. Try again.", isSuccess: false)
        }
    }

    private func parseUsdcMicros(_ raw: String) -> Int64? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let value = Double(trimmed), value > 0 else { return nil }
        return Int64((value * 1_000_000.0).rounded())
    }

    private func formatUsdc(_ micros: Int64) -> String {
        String(format: "$%.2f", Double(micros) / 1_000_000.0)
    }

    private func formatUsdAmount(_ micros: Int64) -> String {
        String(format: "%.2f", Double(micros) / 1_000_000.0)
    }
}
