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
    @State private var toast: MonacoToast?
    @FocusState private var amountFocused: Bool

    private static let posix = Locale(identifier: "en_US_POSIX")

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                if hasStake {
                    amountEntry
                    Text("We sell this much of your slice and move the cash to your account balance. You stay in the cabal.")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.muted)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 12)
                        .accessibilityIdentifier("sell-cabal-explainer")
                } else {
                    VStack(spacing: 8) {
                        Text("Nothing to cash out yet")
                            .font(.body.weight(.semibold))
                            .foregroundStyle(MonacoTheme.ink)
                        Text("Add money to this cabal first. Your slice shows up here.")
                            .font(.subheadline)
                            .foregroundStyle(MonacoTheme.muted)
                            .multilineTextAlignment(.center)
                    }
                    .padding(.top, 48)
                    .accessibilityIdentifier("sell-cabal-empty")
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 24)
        }
        .scrollDismissesKeyboard(.never)
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if hasStake {
                VStack(spacing: 0) {
                    Rectangle().fill(MonacoTheme.hairline).frame(height: 0.5)
                    Button {
                        Task { await submitSell() }
                    } label: {
                        Text(ctaTitle)
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSubmitting || !canSubmit)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 12)
                    .accessibilityIdentifier("sell-cabal-submit-button")
                }
                .background(MonacoTheme.canvas)
            }
        }
        .navigationTitle("Cash out")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .onAppear {
            if amountText.isEmpty, hasStake {
                applyFraction(0.5)
            }
            amountFocused = hasStake
        }
    }

    // MARK: - Amount entry

    private var amountEntry: some View {
        VStack(spacing: 16) {
            ZStack {
                TextField("", text: $amountText)
                    .keyboardType(.decimalPad)
                    .focused($amountFocused)
                    .opacity(0.02)
                    .frame(width: 1, height: 1)
                    .accessibilityIdentifier("sell-cabal-amount-field")
                Text(UsdAmountFormatter.format(micros: selectedUsdMicros))
                    .font(.system(size: 44, weight: .semibold).monospacedDigit())
                    .foregroundStyle(amountText.isEmpty ? MonacoTheme.muted : MonacoTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    .frame(maxWidth: .infinity)
                    .contentShape(Rectangle())
                    .onTapGesture { amountFocused = true }
                    .accessibilityIdentifier("sell-cabal-amount-display")
            }

            HStack(spacing: 8) {
                presetChip("25%", fraction: 0.25, id: 25)
                presetChip("50%", fraction: 0.5, id: 50)
                presetChip("All", fraction: 1, id: 100)
            }

            Text(helperText)
                .font(.footnote)
                .foregroundStyle(isOverLimit ? MonacoTheme.loss : MonacoTheme.muted)
                .accessibilityIdentifier("sell-cabal-helper")
        }
    }

    private func presetChip(_ title: String, fraction: Double, id: Int) -> some View {
        let selected = selectedUsdMicros == StakeWithdrawConverter.usdMicros(forFraction: fraction, maxUsdMicros: maxEquityUsdMicros)
        return Button {
            applyFraction(fraction)
        } label: {
            Text(title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(selected ? MonacoTheme.primaryButtonLabel : MonacoTheme.ink)
                .frame(maxWidth: .infinity, minHeight: 44)
                .background(Capsule().fill(selected ? MonacoTheme.primaryButtonFill : MonacoTheme.surface))
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("sell-cabal-preset-\(id)")
    }

    // MARK: - Derived values

    private var hasStake: Bool {
        maxShareUnits > 0 && maxEquityUsdMicros > 0
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

    private var helperText: String {
        isOverLimit
            ? "More than your slice"
            : "Your slice is worth \(UsdAmountFormatter.format(micros: maxEquityUsdMicros))"
    }

    private var ctaTitle: String {
        if isSubmitting { return "Cashing out…" }
        guard selectedUsdMicros > 0 else { return "Cash out" }
        return "Cash out \(UsdAmountFormatter.format(micros: selectedUsdMicros))"
    }

    private func applyFraction(_ fraction: Double) {
        let micros = StakeWithdrawConverter.usdMicros(forFraction: fraction, maxUsdMicros: maxEquityUsdMicros)
        amountText = editableAmountText(for: micros)
    }

    private func editableAmountText(for micros: Int64) -> String {
        let value = Decimal(micros) / Decimal(1_000_000)
        return NSDecimalNumber(decimal: value).description(withLocale: Self.posix)
    }

    // MARK: - Submit

    private func submitSell() async {
        guard let token = auth.accessToken, canSubmit else { return }
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
                shareAmountMicros: shareAmount
            )
            let success = MonacoToast(
                message: "Cashing out \(UsdAmountFormatter.format(micros: soldMicros)). It lands in your balance in about a minute",
                isSuccess: true
            )
            await onSold()
            if let onToast {
                onToast(success)
                dismiss()
            } else {
                toast = success
            }
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(message: "Cash out at least $1.")
        } catch MonacoAPIError.httpStatus(409) {
            toast = MonacoToast(message: "Your last cash out is still finishing. Try again in a minute")
        } catch MonacoAPIError.httpStatus {
            toast = MonacoToast(message: "Couldn't cash out. Try again")
        } catch let error as URLError where error.code == .notConnectedToInternet {
            toast = MonacoToast(message: "No connection. Check your internet and try again")
        } catch {
            toast = MonacoToast(message: "Couldn't cash out. Try again")
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
