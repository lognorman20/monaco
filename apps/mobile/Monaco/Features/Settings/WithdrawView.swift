import MonacoCore
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

    private var maxDollars: Decimal? {
        guard let micros = balance?.availableUsdcMicros, micros > 0 else { return nil }
        return Decimal(micros) / Decimal(1_000_000)
    }

    private var canContinue: Bool {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return false }
        if let maxDollars, value > maxDollars { return false }
        return !destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                if isLoadingBalance {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.xl)
                } else {
                    AmountEntry(
                        amountText: $amountText,
                        max: maxDollars,
                        presets: [.fraction(1, label: "Max")],
                        helper: balanceHelper
                    )

                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        MonacoSectionHeader("Destination")
                        MonacoTextField("USDC address on Solana", text: $destinationAddress, keyboard: .asciiCapable)
                            .accessibilityIdentifier("withdraw-address-field")
                    }
                }

                if let errorMessage, balance == nil, !isLoadingBalance {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if !isLoadingBalance {
                BottomCTA {
                    Button("Continue") {
                        showConfirm = true
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(!canContinue)
                    .accessibilityIdentifier("withdraw-continue-button")
                }
            }
        }
        .navigationTitle("Cash out")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast, bottomInset: 72)
        .navigationDestination(isPresented: $showConfirm) {
            WithdrawConfirmView(
                destinationAddress: destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines),
                amountText: amountText,
                isSubmitting: isSubmitting,
                onConfirm: { Task { await submitWithdrawal() } }
            )
        }
        .task(id: auth.accessToken) {
            await loadBalance()
        }
    }

    private var balanceHelper: String {
        guard let balance else { return "" }
        return "\(UsdAmountFormatter.format(micros: balance.availableUsdcMicros)) available"
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
        } catch MonacoAPIError.httpStatus {
            errorMessage = "Couldn't load your balance. Pull down to try again."
            balance = nil
        } catch {
            errorMessage = "No connection. Check your internet and try again."
            balance = nil
        }
        isLoadingBalance = false
    }

    private func submitWithdrawal() async {
        guard let token = auth.accessToken else { return }
        guard let value = AmountEntryText.decimal(amountText), value > 0 else {
            toast = MonacoToast(message: "Enter a valid amount.", isSuccess: false)
            return
        }
        var rounded = Decimal()
        var scaled = value * 1_000_000
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        let micros = (rounded as NSDecimalNumber).int64Value

        let address = destinationAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !address.isEmpty else {
            toast = MonacoToast(message: "Enter a destination address.", isSuccess: false)
            return
        }
        if let available = balance?.availableUsdcMicros, micros > available {
            toast = MonacoToast(
                message: "Withdraw less, or cash out of a cabal to your balance first.",
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
            Haptics.success()
            toast = MonacoToast(message: "Cashing out. It lands in about a minute.", isSuccess: true)
            destinationAddress = ""
            amountText = ""
            showConfirm = false
            await loadBalance()
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(
                message: "Withdraw less, or cash out of a cabal to your balance first.",
                isSuccess: false
            )
        } catch MonacoAPIError.httpStatus(409) {
            toast = MonacoToast(message: "A withdrawal is already in progress.", isSuccess: false)
        } catch MonacoAPIError.httpStatus {
            toast = MonacoToast(message: "Couldn't withdraw. Try again.", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "No connection. Check your internet and try again.", isSuccess: false)
        }
    }
}

private struct WithdrawConfirmView: View {
    let destinationAddress: String
    let amountText: String
    let isSubmitting: Bool
    let onConfirm: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Amount")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    MoneyText(decimalString: amountText, style: .large)
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("Destination")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    MonacoWalletAddressText(address: destinationAddress)
                }

                Text("Double-check this address. Transfers can't be undone.")
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.warning)
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button(isSubmitting ? "Sending…" : "Cash out") {
                    onConfirm()
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isSubmitting)
                .accessibilityIdentifier("withdraw-confirm-button")
            }
        }
        .navigationTitle("Confirm")
        .navigationBarTitleDisplayMode(.inline)
    }
}
