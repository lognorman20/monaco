import SwiftUI

/// Add money flow with sweep status feedback (not member balance as money).
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var amountText = ""
    @State private var depositId: String?
    @State private var pollState = DepositPollStateMachine()
    @State private var shareUnits: Int64?
    @State private var errorMessage: String?
    @State private var isSubmitting = false
    @State private var isPolling = false

    var body: some View {
        Form {
            Section {
                Text("Add USDC to grow your club's pot. We track sweep progress until your share is credited.")
                    .monacoSecondaryCaption()
            }

            Section("Amount") {
                TextField("USDC amount", text: $amountText)
                    .keyboardType(.decimalPad)
                    .monacoFormTextField()
                    .disabled(isSubmitting || depositId != nil)
                    .accessibilityIdentifier("deposit-amount-field")
            }

            Section {
                Button(isSubmitting ? "Starting…" : "Add money") {
                    Task { await createDeposit() }
                }
                .monacoFormPrimaryAction()
                .disabled(isSubmitting || depositId != nil || parsedAmountMicro == nil)
                .accessibilityIdentifier("create-deposit-button")
            }

            if depositId != nil {
                Section("Sweep status") {
                    Label(pollState.statusCopy, systemImage: sweepIcon)
                        .font(.subheadline)
                        .foregroundStyle(sweepStatusColor)
                        .accessibilityIdentifier("deposit-sweep-status")

                    if let shareUnits, pollState.phase == .credited {
                        Text("Share units credited: \(shareUnits)")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(MonacoTheme.secondaryText)
                    }

                    if !pollState.isTerminal {
                        Button(isPolling ? "Checking…" : "Refresh status") {
                            Task { await refreshStatus() }
                        }
                        .monacoFormSecondaryAction()
                        .disabled(isPolling || auth.accessToken == nil)
                        .accessibilityIdentifier("deposit-refresh-status")
                    }
                }
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Add money")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var sweepStatusColor: Color {
        switch pollState.phase {
        case .credited:
            MonacoTheme.success
        case .failed:
            MonacoTheme.warning
        case .idle, .awaitingSweep:
            MonacoTheme.primaryText
        }
    }

    private var sweepIcon: String {
        switch pollState.phase {
        case .idle, .awaitingSweep:
            "arrow.triangle.2.circlepath"
        case .credited:
            "checkmark.circle.fill"
        case .failed:
            "exclamationmark.triangle.fill"
        }
    }

    private var parsedAmountMicro: Int64? {
        let trimmed = amountText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty,
              let decimal = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")),
              decimal > 0 else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        let microUnits = (rounded as NSDecimalNumber).int64Value
        return microUnits > 0 ? microUnits : nil
    }

    private func createDeposit() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Sign in to add money."
            return
        }
        guard let amount = parsedAmountMicro else {
            errorMessage = "Enter a positive USDC amount."
            return
        }

        isSubmitting = true
        errorMessage = nil
        pollState = DepositPollStateMachine()

        do {
            let created = try await apiClient.createDeposit(accessToken: accessToken, groupId: groupId, amount: amount)
            depositId = created.depositId
            pollState.apply(status: created.status)
            isSubmitting = false
            if !pollState.isTerminal {
                await refreshStatus()
            }
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not start deposit (HTTP \(status))."
            isSubmitting = false
        } catch {
            errorMessage = "Could not start deposit."
            isSubmitting = false
        }
    }

    private func refreshStatus() async {
        guard let accessToken = auth.accessToken, let depositId else { return }

        isPolling = true
        errorMessage = nil

        do {
            let status = try await apiClient.getDeposit(accessToken: accessToken, depositId: depositId)
            pollState.apply(status: status.status)
            shareUnits = status.shareUnits
        } catch {
            errorMessage = "Could not refresh sweep status."
        }

        isPolling = false
    }

}

#Preview {
    NavigationStack {
        DepositView(auth: PrivyAuthService(), groupId: "00000000-0000-0000-0000-000000000001")
    }
}
