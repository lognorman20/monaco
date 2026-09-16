import SwiftUI

/// M2 deposit screen: POST deposit then poll GET status until confirmed.
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var profile: MeResponse?
    @State private var amountText = ""
    @State private var depositId: String?
    @State private var deposit: GetDepositResponse?
    @State private var errorMessage: String?
    @State private var isSubmitting = false
    @State private var isPolling = false
    @State private var isLoadingProfile = true

    var body: some View {
        Form {
            if isLoadingProfile {
                Section {
                    ProgressView("Loading member wallet…")
                }
            } else if let profile {
                Section("Member wallet") {
                    detailRow(title: "Solana address", value: profile.memberWalletAddress, monospaced: true)
                        .accessibilityIdentifier("deposit-member-wallet")
                    Text("Fund this address with USDC, then create a deposit below.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }

            Section("Group") {
                Text(groupId)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
            }

            Section("Deposit amount (USDC)") {
                TextField("Amount", text: $amountText)
                    .keyboardType(.decimalPad)
                    .disabled(isSubmitting || depositId != nil)
                    .accessibilityIdentifier("deposit-amount-field")
                if let microUnits = parsedAmountMicro {
                    Text("= \(microUnits) micro-units")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            Section {
                Button(isSubmitting ? "Submitting…" : "Create deposit") {
                    Task { await createDeposit() }
                }
                .disabled(isSubmitting || depositId != nil || parsedAmountMicro == nil)
                .accessibilityIdentifier("create-deposit-button")

                if depositId != nil {
                    Button(isPolling ? "Refreshing…" : "Refresh status") {
                        Task { await refreshStatus() }
                    }
                    .disabled(isPolling || auth.accessToken == nil)
                }
            }

            if let deposit {
                Section("Deposit status") {
                    detailRow(title: "Deposit ID", value: deposit.depositId)
                    detailRow(title: "Amount", value: formatUSDC(microUnits: deposit.amount))
                    detailRow(title: "Status", value: deposit.status)
                    detailRow(title: "Share units", value: String(deposit.shareUnits))
                    if let txSignature = deposit.txSignature, !txSignature.isEmpty {
                        detailRow(title: "Tx signature", value: txSignature, monospaced: true)
                    }
                }
            } else if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Deposit")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: auth.accessToken) {
            await loadProfile()
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

    private func formatUSDC(microUnits: Int64) -> String {
        let dollars = Decimal(microUnits) / Decimal(1_000_000)
        return "\(dollars) USDC"
    }

    @ViewBuilder
    private func detailRow(title: String, value: String, monospaced: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(monospaced ? .body.monospaced() : .body)
                .textSelection(.enabled)
        }
    }

    private func loadProfile() async {
        guard let accessToken = auth.accessToken else {
            profile = nil
            isLoadingProfile = false
            return
        }

        isLoadingProfile = true
        do {
            _ = try await apiClient.openSession(accessToken: accessToken)
            profile = try await apiClient.me(accessToken: accessToken)
        } catch {
            profile = nil
        }
        isLoadingProfile = false
    }

    private func createDeposit() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing Privy access token."
            return
        }
        guard let amount = parsedAmountMicro else {
            errorMessage = "Enter a positive USDC amount."
            return
        }

        isSubmitting = true
        errorMessage = nil

        do {
            let created = try await apiClient.createDeposit(accessToken: accessToken, groupId: groupId, amount: amount)
            depositId = created.depositId
            isSubmitting = false
            await refreshStatus()
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Create deposit failed (HTTP \(status))."
            isSubmitting = false
        } catch {
            errorMessage = "Could not create deposit."
            isSubmitting = false
        }
    }

    private func refreshStatus() async {
        guard let accessToken = auth.accessToken, let depositId else {
            return
        }

        isPolling = true
        errorMessage = nil

        do {
            let status = try await apiClient.getDeposit(accessToken: accessToken, depositId: depositId)
            deposit = status
        } catch {
            errorMessage = "Could not load deposit status."
        }

        isPolling = false
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: PrivyAuthService(), groupId: "00000000-0000-0000-0000-000000000001")
    }
}
