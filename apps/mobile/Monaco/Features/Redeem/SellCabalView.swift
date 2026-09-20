import MonacoCore
import SwiftUI

/// Cash out: sell part (or all) of your slice back to your account balance. You stay in the cabal.
struct SellCabalView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let maxShareUnits: Int64
    let equityUsd: String
    var onSold: () async -> Void = {}
    /// When set, the success toast is handed to the presenting screen and this screen closes.
    var onToast: ((MonacoToast) -> Void)? = nil

    private let apiClient = MonacoAPIClient()

    @Environment(\.dismiss) private var dismiss
    @State private var amountText = ""
    @State private var isSubmitting = false
    /// Idempotency key for the cash out in flight; a retry after a lost response reuses it.
    @State private var sellSubmission = IdempotentSubmission()
    @State private var toast: MonacoToast?

    var body: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.l) {
                if hasStake {
                    AmountEntry(
                        amountText: $amountText,
                        max: maxEquityUsd,
                        presets: [
                            .fraction(0.25, label: "25%"),
                            .fraction(0.5, label: "50%"),
                            .fraction(1, label: "All"),
                        ],
                        helper: helperLine,
                        overLimitHelper: "More than your slice",
                        problem: CashOutAmountRule.problem(for: verdict),
                        showsKeyboardDoneButton: true
                    )
                    .padding(.top, MonacoTheme.Space.xl)
                    .accessibilityIdentifier("sell-cabal-amount-display")
                    Text(CashOutAmountRule.explainer(for: verdict))
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, MonacoTheme.Space.sm)
                        .accessibilityIdentifier("sell-cabal-explainer")
                } else if sliceIsTooSmall {
                    EmptyState(
                        title: "Too small to cash out",
                        message: "Your slice is worth \(UsdAmountFormatter.format(micros: maxEquityUsdMicros)). Cash out starts at \(UsdAmountFormatter.format(micros: RedeemDustMinimum.usdcMicros)), so this one has to grow first."
                    )
                    .padding(.top, 48)
                    .accessibilityIdentifier("sell-cabal-below-minimum")
                } else {
                    EmptyState(
                        title: "Nothing to cash out yet",
                        message: "Add money to this cabal first. Your slice shows up here."
                    )
                    .padding(.top, 48)
                    .accessibilityIdentifier("sell-cabal-empty")
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
        }
        .scrollDismissesKeyboard(.never)
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if hasStake {
                BottomCTA {
                    Button {
                        Task { await submitSell() }
                    } label: {
                        if isSubmitting {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Text(ctaTitle)
                        }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSubmitting || !canSubmit)
                    .accessibilityIdentifier("sell-cabal-submit-button")
                }
            }
        }
        .navigationTitle("Cash out")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast, bottomInset: 72)
    }

    // MARK: - Derived values

    /// A slice worth enough to be sold. One worth less than the floor gets its own explanation
    /// rather than a keypad where every amount is refused.
    private var hasStake: Bool {
        maxShareUnits > 0 && maxEquityUsdMicros >= RedeemDustMinimum.usdcMicros
    }

    private var sliceIsTooSmall: Bool {
        maxShareUnits > 0 && CashOutAmountRule.sliceIsBelowMinimum(sliceMicros: maxEquityUsdMicros)
    }

    private var maxEquityUsd: Decimal {
        Decimal(maxEquityUsdMicros) / Decimal(1_000_000)
    }

    private var maxEquityUsdMicros: Int64 {
        StakeWithdrawConverter.usdMicros(fromDecimalString: equityUsd) ?? 0
    }

    private var enteredUsdMicros: Int64 {
        AmountEntryText.micros(amountText) ?? 0
    }

    private var verdict: CashOutAmountRule.Verdict {
        CashOutAmountRule.verdict(enteredMicros: enteredUsdMicros, sliceMicros: maxEquityUsdMicros)
    }

    /// What will actually be sold: the typed amount, or the whole slice when leaving the
    /// remainder behind would strand it.
    private var selectedUsdMicros: Int64 {
        CashOutAmountRule.effectiveMicros(
            for: verdict,
            enteredMicros: enteredUsdMicros,
            sliceMicros: maxEquityUsdMicros
        )
    }

    private var selectedShareUnits: Int64 {
        StakeWithdrawConverter.shareMicros(
            forUsdMicros: selectedUsdMicros,
            totalEquityUsdMicros: maxEquityUsdMicros,
            maxShareMicros: maxShareUnits
        ) ?? 0
    }

    private var helperLine: String {
        CashOutAmountRule.note(for: verdict, sliceMicros: maxEquityUsdMicros)
            ?? "Your slice is worth \(UsdAmountFormatter.format(micros: maxEquityUsdMicros))"
    }

    private var canSubmit: Bool {
        CashOutAmountRule.maySubmit(verdict)
            && selectedShareUnits > 0
            && selectedShareUnits <= maxShareUnits
    }

    private var ctaTitle: String {
        if isSubmitting { return "Cashing out…" }
        // An amount the screen won't take never appears on the button.
        guard CashOutAmountRule.maySubmit(verdict), selectedUsdMicros > 0 else { return "Cash out" }
        return "Cash out \(UsdAmountFormatter.format(micros: selectedUsdMicros))"
    }

    // MARK: - Submit

    private func submitSell() async {
        // The disabled state only lands on the next render; a second tap in the same frame
        // must not sell the slice twice.
        guard !isSubmitting, let token = auth.accessToken, canSubmit else { return }
        // A full exit sends no share amount, so the backend closes the position outright. That is
        // also how a sale that would have stranded a sub-floor remainder goes out.
        guard let sale = CashOutAmountRule.sale(for: verdict, selectedShareUnits: selectedShareUnits) else { return }
        isSubmitting = true
        defer { isSubmitting = false }

        let soldMicros = selectedUsdMicros

        do {
            _ = try await apiClient.withdrawToBalance(
                accessToken: token,
                groupId: groupId,
                shareAmountMicros: sale.shareAmountMicros,
                submission: sellSubmission
            )
            let success = MonacoToast(
                message: "Cashing out \(UsdAmountFormatter.format(micros: soldMicros)). It lands in your balance in about a minute",
                isSuccess: true
            )
            Haptics.success()
            await onSold()
            if let onToast {
                onToast(success)
                dismiss()
            } else {
                toast = success
            }
        } catch {
            if error.isRequestCancellation { return }
            toast = MonacoToast(message: MoneyFlowCopy.sellStakeFailure(FlowErrorInput(error)).summary)
        }
    }
}
