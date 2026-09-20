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
    private let dustGate = RedeemSliderGate()

    @Environment(\.dismiss) private var dismiss
    @State private var amountText = ""
    @State private var isSubmitting = false
    /// Idempotency key for the cash out in flight; a retry after a lost response reuses it.
    @State private var sellSubmission = IdempotentSubmission()
    @State private var toast: MonacoToast?

    private static let posix = Locale(identifier: "en_US_POSIX")

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
                        helper: "Your slice is worth \(UsdAmountFormatter.format(micros: maxEquityUsdMicros))",
                        overLimitHelper: "More than your slice"
                    )
                    .padding(.top, MonacoTheme.Space.xl)
                    .accessibilityIdentifier("sell-cabal-amount-display")
                    Text("We sell this much of your slice and move the cash to your account balance. You stay in the cabal.")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, MonacoTheme.Space.sm)
                        .accessibilityIdentifier("sell-cabal-explainer")
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

    private var hasStake: Bool {
        maxShareUnits > 0 && maxEquityUsdMicros > 0
    }

    private var maxEquityUsd: Decimal {
        Decimal(maxEquityUsdMicros) / Decimal(1_000_000)
    }

    private var maxEquityUsdMicros: Int64 {
        StakeWithdrawConverter.usdMicros(fromDecimalString: equityUsd) ?? 0
    }

    private var enteredUsdMicros: Int64 {
        parseUsdcMicros(amountText) ?? 0
    }

    /// What will actually be sold: never more than the slice.
    private var selectedUsdMicros: Int64 {
        min(enteredUsdMicros, maxEquityUsdMicros)
    }

    private var isOverLimit: Bool {
        enteredUsdMicros > maxEquityUsdMicros
    }

    private var selectedShareUnits: Int64 {
        StakeWithdrawConverter.shareMicros(
            forUsdMicros: selectedUsdMicros,
            totalEquityUsdMicros: maxEquityUsdMicros,
            maxShareMicros: maxShareUnits
        ) ?? 0
    }

    private var canSubmit: Bool {
        !isOverLimit
            && dustGate.maySubmit(selectedMicros: selectedUsdMicros)
            && selectedShareUnits > 0
            && selectedShareUnits <= maxShareUnits
    }

    private var ctaTitle: String {
        if isSubmitting { return "Cashing out…" }
        guard selectedUsdMicros > 0 else { return "Cash out" }
        return "Cash out \(UsdAmountFormatter.format(micros: selectedUsdMicros))"
    }

    // MARK: - Submit

    private func submitSell() async {
        // The disabled state only lands on the next render; a second tap in the same frame
        // must not sell the slice twice.
        guard !isSubmitting, let token = auth.accessToken, canSubmit else { return }
        isSubmitting = true
        defer { isSubmitting = false }

        let soldMicros = selectedUsdMicros
        let shareAmount = StakeWithdrawConverter.isFullWithdraw(
            selectedUsdMicros: soldMicros,
            totalEquityUsdMicros: maxEquityUsdMicros
        ) ? nil : selectedShareUnits

        do {
            _ = try await apiClient.withdrawToBalance(
                accessToken: token,
                groupId: groupId,
                shareAmountMicros: shareAmount,
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

    private func parseUsdcMicros(_ raw: String) -> Int64? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: ",", with: ".")
        guard !trimmed.isEmpty,
              let decimal = Decimal(string: trimmed, locale: Self.posix),
              decimal >= 0 else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }
}
