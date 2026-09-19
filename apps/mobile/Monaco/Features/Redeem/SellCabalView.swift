import SwiftUI
import MonacoCore

/// Sell deployed stake back to account balance without leaving the cabal.
struct SellCabalView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let maxShareUnits: Int64
    let equityUsd: String
    var onSold: () async -> Void = {}

    private let apiClient = MonacoAPIClient()
    private let dustGate = RedeemSliderGate()

    @State private var selectedUsdMicros: Int64 = 0
    @State private var sliderFraction: Double = 1
    @State private var amountText = ""
    @State private var isSubmitting = false
    @State private var toast: MonacoToast?
    @FocusState private var amountFieldFocused: Bool

    var body: some View {
        Form {
            Section {
                Text("USDC returns to your account balance. You stay in this cabal.")
                    .monacoSecondaryCaption()
            }

            Section("Your slice") {
                LabeledContent("Value", value: UsdAmountFormatter.format(decimalString: equityUsd))
                if maxShareUnits <= 0 || maxEquityUsdMicros <= 0 {
                    Text("No stake to sell yet. Fund this cabal first.")
                        .monacoSecondaryCaption()
                }
            }

            if maxShareUnits > 0, maxEquityUsdMicros > 0 {
                Section("Amount") {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(UsdAmountFormatter.format(micros: selectedUsdMicros))
                            .font(.title.bold())
                            .monospacedDigit()
                            .frame(maxWidth: .infinity, alignment: .center)
                            .accessibilityIdentifier("sell-cabal-amount-display")

                        HStack {
                            Text(UsdAmountFormatter.format(micros: 0))
                            Spacer()
                            Text(UsdAmountFormatter.format(micros: maxEquityUsdMicros))
                        }
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)

                        Slider(value: $sliderFraction, in: 0...1)
                            .accessibilityIdentifier("sell-cabal-slider")
                            .onChange(of: sliderFraction) { _, newValue in
                                amountFieldFocused = false
                                applySliderFraction(newValue)
                            }

                        TextField("USDC amount", text: $amountText)
                            .keyboardType(.decimalPad)
                            .monacoFormTextField()
                            .focused($amountFieldFocused)
                            .accessibilityIdentifier("sell-cabal-amount-field")

                        HStack(spacing: 8) {
                            presetButton(title: "25%", fraction: 0.25)
                            presetButton(title: "50%", fraction: 0.5)
                            presetButton(title: "100%", fraction: 1)
                        }
                    }
                    .padding(.vertical, 4)
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                }

                Section {
                    Button(isSubmitting ? "Selling…" : "Sell") {
                        Task { await submitSell() }
                    }
                    .monacoFormPrimaryAction()
                    .disabled(isSubmitting || !canSubmit)
                    .accessibilityIdentifier("sell-cabal-submit-button")
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Sell")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .onAppear {
            applySliderFraction(1)
        }
        .onChange(of: amountFieldFocused) { _, focused in
            guard !focused else { return }
            applyAmountText()
        }
    }

    private var maxEquityUsdMicros: Int64 {
        StakeWithdrawConverter.usdMicros(fromDecimalString: equityUsd) ?? 0
    }

    private var selectedShareUnits: Int64 {
        StakeWithdrawConverter.shareMicros(
            forUsdMicros: selectedUsdMicros,
            totalEquityUsdMicros: maxEquityUsdMicros,
            maxShareMicros: maxShareUnits
        ) ?? 0
    }

    private var canSubmit: Bool {
        dustGate.maySubmit(selectedMicros: selectedUsdMicros)
            && selectedShareUnits > 0
            && selectedShareUnits <= maxShareUnits
    }

    @ViewBuilder
    private func presetButton(title: String, fraction: Double) -> some View {
        Button(title) {
            amountFieldFocused = false
            applySliderFraction(fraction)
        }
        .monacoFormSecondaryAction()
        .accessibilityIdentifier("sell-cabal-preset-\(Int(fraction * 100))")
    }

    private func applySliderFraction(_ fraction: Double) {
        let clamped = min(1, max(0, fraction))
        sliderFraction = clamped
        selectedUsdMicros = StakeWithdrawConverter.usdMicros(
            forFraction: clamped,
            maxUsdMicros: maxEquityUsdMicros
        )
        amountText = editableAmountText(for: selectedUsdMicros)
    }

    private func applyAmountText() {
        guard let micros = parseUsdcMicros(amountText) else {
            amountText = editableAmountText(for: selectedUsdMicros)
            return
        }
        selectedUsdMicros = min(micros, maxEquityUsdMicros)
        sliderFraction = StakeWithdrawConverter.fraction(
            forUsdMicros: selectedUsdMicros,
            maxUsdMicros: maxEquityUsdMicros
        )
        amountText = editableAmountText(for: selectedUsdMicros)
    }

    private func editableAmountText(for micros: Int64) -> String {
        String(format: "%.2f", Double(micros) / 1_000_000.0)
    }

    private func submitSell() async {
        amountFieldFocused = false
        applyAmountText()
        guard let token = auth.accessToken, canSubmit else { return }
        isSubmitting = true
        defer { isSubmitting = false }

        let shareAmount = StakeWithdrawConverter.isFullWithdraw(
            selectedUsdMicros: selectedUsdMicros,
            totalEquityUsdMicros: maxEquityUsdMicros
        ) ? nil : selectedShareUnits

        do {
            _ = try await apiClient.withdrawToBalance(
                accessToken: token,
                groupId: groupId,
                shareAmountMicros: shareAmount
            )
            toast = MonacoToast(message: "USDC moved to your account balance.", isSuccess: true)
            await onSold()
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(
                message: "Amount too small to sell stock, or below $0.10 minimum. Try more USDC or a larger amount.",
                isSuccess: false
            )
        } catch MonacoAPIError.httpStatus(409) {
            toast = MonacoToast(message: "Payout still finishing. Wait a moment and try again.", isSuccess: false)
        } catch MonacoAPIError.httpStatus(let code) {
            toast = MonacoToast(message: "Could not sell (HTTP \(code)).", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "Could not complete sell. Try again.", isSuccess: false)
        }
    }

    private func parseUsdcMicros(_ raw: String) -> Int64? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let decimal = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")),
              decimal >= 0 else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }
}
