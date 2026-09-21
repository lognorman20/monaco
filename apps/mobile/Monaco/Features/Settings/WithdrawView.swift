import MonacoCore
import SwiftUI

/// Send available account USDC to an external Base address.
struct WithdrawView: View {
    @ObservedObject var auth: DynamicAuthService

    private let apiClient = MonacoAPIClient()

    @StateObject private var balanceLoader = PlatformBalanceLoader()
    @State private var destinationAddress = ""
    @State private var amountText = ""
    @State private var isSubmitting = false
    @State private var showConfirm = false
    /// Shown on the confirm screen, which covers this screen's toast while it is pushed.
    @State private var submitFailure: FlowFailure?
    @State private var toast: MonacoToast?

    private var balance: PlatformBalanceDTO? {
        balanceLoader.balance
    }

    private var maxDollars: Decimal? {
        guard let micros = balance?.availableUsdcMicros, micros > 0 else { return nil }
        return Decimal(micros) / Decimal(1_000_000)
    }

    private var canContinue: Bool {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return false }
        if let maxDollars, value > maxDollars { return false }
        if case .success = addressValidation { return true }
        return false
    }

    private var addressValidation: Result<String, EVMAddressProblem> {
        EVMAddress.validate(destinationAddress, ownDepositAddress: balance?.memberWalletAddress)
    }

    /// Nothing while the field is empty; otherwise why the pasted address can't be used.
    private var addressProblemMessage: String? {
        guard case .failure(let problem) = addressValidation, problem != .empty else { return nil }
        return EVMAddress.message(for: problem)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                switch balanceLoader.phase {
                case .loading:
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.xl)
                        .accessibilityIdentifier("withdraw-loading")
                case .failed(let message):
                    EmptyState(
                        title: "Balance unavailable",
                        message: message,
                        actionTitle: "Try again",
                        action: { Task { await balanceLoader.load(accessToken: auth.accessToken) } }
                    )
                    .padding(.top, MonacoTheme.Space.xl)
                    .accessibilityIdentifier("withdraw-balance-error")
                case .loaded:
                    AmountEntry(
                        amountText: $amountText,
                        max: maxDollars,
                        presets: [.fraction(1, label: "Max")],
                        helper: balanceHelper,
                        showsKeyboardDoneButton: true
                    )

                    VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                        MonacoSectionHeader("Destination")
                        MonacoTextField("USDC address on Base", text: $destinationAddress, keyboard: .asciiCapable)
                            .accessibilityIdentifier("withdraw-address-field")
                        if let addressProblemMessage {
                            Text(addressProblemMessage)
                                .font(MonacoTheme.Typo.caption)
                                .foregroundStyle(MonacoTheme.warning)
                                .accessibilityIdentifier("withdraw-address-problem")
                        }
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        // The decimal pad covers the destination field, and a decimal pad has no return key:
        // dragging the list is the member's way back to the address.
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if showsForm {
                BottomCTA {
                    Button("Continue") {
                        submitFailure = nil
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
                failure: submitFailure,
                onConfirm: { Task { await submitWithdrawal() } }
            )
        }
        .task(id: auth.accessToken) {
            await balanceLoader.load(accessToken: auth.accessToken)
        }
    }

    /// The amount pad, the destination field and the button belong together: whenever one is on
    /// screen, so are the others. A reload never takes them away mid-entry.
    private var showsForm: Bool {
        if case .loaded = balanceLoader.phase { return true }
        return false
    }

    private var balanceHelper: String {
        guard let balance else { return "" }
        return "\(UsdAmountFormatter.format(micros: balance.availableUsdcMicros)) available"
    }

    private func submitWithdrawal() async {
        // The disabled state only lands on the next render; a second tap in the same frame
        // must not start a second transfer.
        guard !isSubmitting else { return }
        guard let token = auth.accessToken else { return }
        guard let value = AmountEntryText.decimal(amountText), value > 0,
              let micros = AmountEntryText.micros(amountText) else {
            toast = MonacoToast(message: "Enter a valid amount.", isSuccess: false)
            return
        }

        guard case .success(let address) = addressValidation else { return }
        if let available = balance?.availableUsdcMicros, micros > available {
            submitFailure = MoneyFlowCopy.cashOutFailure(
                FlowErrorInput(status: 400, serverMessage: "amount exceeds available platform balance")
            )
            return
        }

        isSubmitting = true
        submitFailure = nil
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
            await balanceLoader.load(accessToken: token)
        } catch {
            if error.isRequestCancellation { return }
            submitFailure = MoneyFlowCopy.cashOutFailure(FlowErrorInput(error))
            Haptics.warning()
        }
    }
}

private struct WithdrawConfirmView: View {
    let destinationAddress: String
    let amountText: String
    let isSubmitting: Bool
    let failure: FlowFailure?
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

                if let failure {
                    VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                        Text(failure.message)
                            .font(MonacoTheme.Typo.body)
                            .foregroundStyle(MonacoTheme.ink)
                        if let nextStep = failure.nextStep {
                            Text(nextStep)
                                .font(MonacoTheme.Typo.callout)
                                .foregroundStyle(MonacoTheme.muted)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("withdraw-confirm-failure")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button(isSubmitting ? "Sending…" : failure?.isRetryable == true ? "Try again" : "Cash out") {
                    onConfirm()
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isSubmitting || failure?.isRetryable == false)
                .accessibilityIdentifier("withdraw-confirm-button")
            }
        }
        .navigationTitle("Confirm")
        .navigationBarTitleDisplayMode(.inline)
    }
}
