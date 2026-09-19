import SwiftUI

/// Send available account USDC to an external Solana wallet.
struct WithdrawView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var balance: PlatformBalanceDTO?
    @State private var destinationAddress = ""
    @State private var amountText = ""
    @State private var isLoadingBalance = true
    @State private var isSubmitting = false
    @State private var showConfirm = false
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
                        .accessibilityIdentifier("withdraw-available-balance")
                } else {
                    Text(errorMessage ?? "Could not load account balance.")
                        .foregroundStyle(MonacoTheme.warning)
                }
            } header: {
                Text("Account balance")
            }

            Section("Destination") {
                TextField("Solana wallet address", text: $destinationAddress)
                    .monacoWalletAddressField()
                    .accessibilityIdentifier("withdraw-address-field")
            }

            Section("Amount") {
                TextField("USDC amount", text: $amountText)
                    .keyboardType(.decimalPad)
                    .accessibilityIdentifier("withdraw-amount-field")
                if let maxMicros = balance?.availableUsdcMicros, maxMicros > 0 {
                    Button("Max") {
                        amountText = String(format: "%.2f", Double(maxMicros) / 1_000_000.0)
                    }
                    .monacoFormSecondaryAction()
                    .accessibilityIdentifier("withdraw-max-button")
                }
            }

            Section {
                Button("Continue") {
                    showConfirm = true
                }
                .monacoFormPrimaryAction()
                .disabled(!canContinue)
                .accessibilityIdentifier("withdraw-continue-button")
            }
        }
        .monacoFormScreen()
        .navigationTitle("Withdraw")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .navigationDestination(isPresented: $showConfirm) {
            WithdrawConfirmView(
                destinationAddress: destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines),
                amountMicros: parsedAmountMicros,
                formattedAmount: formattedAmount,
                isSubmitting: isSubmitting,
                onConfirm: { Task { await submitWithdrawal() } }
            )
        }
        .task(id: auth.accessToken) {
            await loadBalance()
        }
    }

    private var parsedAmountMicros: Int64? {
        parseUsdcMicros(amountText)
    }

    private var formattedAmount: String {
        guard let micros = parsedAmountMicros else { return amountText }
        return formatUsdc(micros)
    }

    private var canContinue: Bool {
        guard balance != nil else { return false }
        guard let micros = parsedAmountMicros, micros > 0 else { return false }
        let address = destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        return !address.isEmpty
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

    private func submitWithdrawal() async {
        guard let token = auth.accessToken else { return }
        guard let micros = parsedAmountMicros, micros > 0 else {
            toast = MonacoToast(message: "Enter a valid USDC amount.", isSuccess: false)
            return
        }
        let address = destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !address.isEmpty else {
            toast = MonacoToast(message: "Enter a destination address.", isSuccess: false)
            return
        }
        if let available = balance?.availableUsdcMicros, micros > available {
            toast = MonacoToast(
                message: "Withdraw less or move cabal stake to your balance first.",
                isSuccess: false
            )
            return
        }

        isSubmitting = true
        defer { isSubmitting = false }

        do {
            _ = try await apiClient.createPlatformWithdrawal(
                accessToken: token,
                amount: micros,
                toAddress: address
            )
            toast = MonacoToast(message: "Withdrawal sent", isSuccess: true)
            destinationAddress = ""
            amountText = ""
            showConfirm = false
            await loadBalance()
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(
                message: "Withdraw less or move cabal stake to your balance first.",
                isSuccess: false
            )
        } catch MonacoAPIError.httpStatus(409) {
            toast = MonacoToast(message: "A withdrawal is already in progress.", isSuccess: false)
        } catch MonacoAPIError.httpStatus(let status) {
            toast = MonacoToast(message: "Could not withdraw (HTTP \(status)).", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "Could not withdraw. Try again.", isSuccess: false)
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
}

private struct WithdrawConfirmView: View {
    let destinationAddress: String
    let amountMicros: Int64?
    let formattedAmount: String
    let isSubmitting: Bool
    let onConfirm: () -> Void

    var body: some View {
        Form {
            Section("Review") {
                LabeledContent("Amount", value: formattedAmount)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Destination")
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                    MonacoWalletAddressText(address: destinationAddress)
                }
            }

            Section {
                Text("Double-check this address. Transfers cannot be reversed.")
                    .foregroundStyle(MonacoTheme.warning)
                    .font(.footnote)
            }

            Section {
                Button(isSubmitting ? "Sending…" : "Confirm withdrawal") {
                    onConfirm()
                }
                .monacoFormPrimaryAction()
                .disabled(isSubmitting || amountMicros == nil)
                .accessibilityIdentifier("withdraw-confirm-button")
            }
        }
        .monacoFormScreen()
        .navigationTitle("Confirm")
        .navigationBarTitleDisplayMode(.inline)
    }
}
