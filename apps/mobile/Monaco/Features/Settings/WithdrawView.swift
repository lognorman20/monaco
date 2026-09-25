import MonacoCore
import SwiftUI

/// Cash out to an address: send available account USDC to an external Solana address.
///
/// Owns the balance, the confirm step, the submission and its idempotency key.
/// `WithdrawContent` is the entry layout and `WithdrawConfirmView` the step before sending.
struct WithdrawView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @StateObject private var balanceLoader = PlatformBalanceLoader()
    @State private var destinationAddress = ""
    @State private var amountText = ""
    @State private var isSubmitting = false
    /// Idempotency key for the withdrawal in flight; a retry after a lost response reuses it.
    @State private var withdrawalSubmission = IdempotentSubmission()
    @State private var showConfirm = false
    /// Shown on the confirm screen, which covers this screen's toast while it is pushed.
    @State private var submitFailure: FlowFailure?
    @State private var toast: MonacoToast?

    private var balance: PlatformBalanceDTO? {
        balanceLoader.balance
    }

    private var form: WithdrawForm {
        WithdrawForm(amountText: amountText, destinationAddress: destinationAddress, balance: balance)
    }

    var body: some View {
        WithdrawContent(
            phase: balanceLoader.phase,
            amountText: $amountText,
            destinationAddress: $destinationAddress,
            onContinue: {
                submitFailure = nil
                showConfirm = true
            },
            onRetry: { Task { await balanceLoader.load(accessToken: auth.accessToken) } }
        )
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

        guard case .success(let address) = form.addressValidation else { return }
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
                toAddress: address,
                submission: withdrawalSubmission
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

/// What Cash out says and allows for the amount and address typed against the balance it has.
struct WithdrawForm: Equatable {
    let amountText: String
    let destinationAddress: String
    let availableMicros: Int64?
    /// The member's own deposit address, which is never a destination.
    let ownDepositAddress: String?

    init(amountText: String, destinationAddress: String, balance: PlatformBalanceDTO?) {
        self.amountText = amountText
        self.destinationAddress = destinationAddress
        availableMicros = balance?.availableUsdcMicros
        ownDepositAddress = balance?.memberWalletAddress
    }

    var maxDollars: Decimal? {
        guard let availableMicros, availableMicros > 0 else { return nil }
        return Decimal(availableMicros) / Decimal(1_000_000)
    }

    var addressValidation: Result<String, SolanaAddressProblem> {
        SolanaAddress.validate(destinationAddress, ownDepositAddress: ownDepositAddress)
    }

    var canContinue: Bool {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return false }
        if let maxDollars, value > maxDollars { return false }
        if case .success = addressValidation { return true }
        return false
    }

    /// Nothing while the field is empty; otherwise why the pasted address can't be used.
    var addressProblem: String? {
        guard case .failure(let problem) = addressValidation, problem != .empty else { return nil }
        return SolanaAddress.message(for: problem)
    }

    var balanceHelper: String {
        guard let availableMicros else { return "" }
        return "\(UsdAmountFormatter.format(micros: availableMicros)) available"
    }

    /// Under the address field: what kind of address, and that it is final.
    static let caveat = "A Solana address that accepts USDC. Transfers can't be undone."
}

/// Cash out's entry layout: the amount as the hero, the address it goes to in the market's
/// voice, and the two things to know about that address as captions under it.
///
/// Pure: what the screen knows comes in, what the member does goes out.
struct WithdrawContent: View {
    let phase: PlatformBalanceLoader.Phase
    @Binding var amountText: String
    @Binding var destinationAddress: String
    let onContinue: () -> Void
    let onRetry: () -> Void

    private var form: WithdrawForm {
        if case .loaded(let balance) = phase {
            return WithdrawForm(amountText: amountText, destinationAddress: destinationAddress, balance: balance)
        }
        return WithdrawForm(amountText: amountText, destinationAddress: destinationAddress, balance: nil)
    }

    /// The amount pad, the destination field and the button belong together: whenever one is on
    /// screen, so are the others. A reload never takes them away mid-entry.
    private var showsForm: Bool {
        if case .loaded = phase { return true }
        return false
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                switch phase {
                case .loading:
                    AmountEntrySkeleton(presetCount: 1)
                        .padding(.horizontal, MonacoTheme.Space.gutter)
                        .accessibilityIdentifier("withdraw-loading")
                case .failed(let message):
                    EmptyState(
                        title: "Balance unavailable",
                        message: message,
                        actionTitle: "Try again",
                        action: onRetry
                    )
                    .accessibilityIdentifier("withdraw-balance-error")
                case .loaded:
                    AmountEntry(
                        amountText: $amountText,
                        max: form.maxDollars,
                        presets: [.fraction(1, label: "Max")],
                        helper: form.balanceHelper,
                        showsKeyboardDoneButton: true
                    )
                    .padding(.horizontal, MonacoTheme.Space.gutter)

                    destination
                }
            }
            .padding(.top, MonacoTheme.Space.xl)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        // The decimal pad covers the destination field, and a decimal pad has no return key:
        // dragging the list is the member's way back to the address.
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if showsForm {
                BottomCTA {
                    Button("Continue", action: onContinue)
                        .buttonStyle(.monacoPrimary)
                        .disabled(!form.canContinue)
                        .accessibilityIdentifier("withdraw-continue-button")
                }
            }
        }
        .navigationTitle("Cash out")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var destination: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Send to")

            WithdrawAddressField(text: $destinationAddress)

            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                if let problem = form.addressProblem {
                    Text(problem)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.loss)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("withdraw-address-problem")
                }
                Text(WithdrawForm.caveat)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
    }
}

/// The destination, in the market's voice: it wraps by character and never hyphenates, so a
/// member can read the whole address back before the confirm step shows it again.
private struct WithdrawAddressField: View {
    @Binding var text: String

    private static let placeholder = "USDC address on Solana"

    var body: some View {
        MonacoAddressField(placeholder: Self.placeholder, text: $text, accessibilityIdentifier: "withdraw-address-field")
    }
}

/// The step before the money leaves: how much, where to, and the one thing that cannot be
/// taken back. After a failure it says what happened above the facts, and its button resends
/// only when the copy says that is safe.
struct WithdrawConfirmView: View {
    let destinationAddress: String
    let amountText: String
    let isSubmitting: Bool
    let failure: FlowFailure?
    let onConfirm: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("You're cashing out")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                    MoneyText(decimalString: amountText, style: .hero)
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .accessibilityElement(children: .combine)

                if let failure {
                    failureBlock(failure)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoGroupedList {
                        ReceiptLine(label: "To", value: .address(destinationAddress))
                        ReceiptLine(label: "From", value: .words("Account balance"))
                        ReceiptLine(label: "Arrives", value: .words("About a minute"), isLast: true)
                    }
                    Text("Double-check the address. Transfers can't be undone.")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, MonacoTheme.Space.m)
                }
            }
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

    private func failureBlock(_ failure: FlowFailure) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            Image(systemName: "exclamationmark.circle.fill")
                .font(.body.weight(.semibold))
                .foregroundStyle(MonacoTheme.loss)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xs) {
                Text(failure.message)
                    .font(MonacoTheme.Typo.bodyStrong)
                    .foregroundStyle(MonacoTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                if let nextStep = failure.nextStep {
                    Text(nextStep)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("withdraw-confirm-failure")
    }
}
