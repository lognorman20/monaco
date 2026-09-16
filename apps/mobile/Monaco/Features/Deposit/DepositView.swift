import SwiftUI

/// M2 deposit screen: POST deposit then poll GET status until confirmed.
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var amountText = ""
    @State private var depositId: String?
    @State private var deposit: GetDepositResponse?
    @State private var errorMessage: String?
    @State private var isSubmitting = false
    @State private var isPolling = false

    var body: some View {
        Form {
            Section("Group") {
                Text(groupId)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
            }

            Section("Deposit amount (USDC micro-units)") {
                TextField("Amount", text: $amountText)
                    .keyboardType(.numberPad)
                    .disabled(isSubmitting || depositId != nil)
            }

            Section {
                Button(isSubmitting ? "Submitting…" : "Create deposit") {
                    Task { await createDeposit() }
                }
                .disabled(isSubmitting || depositId != nil || parsedAmount == nil)

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
    }

    private var parsedAmount: Int64? {
        guard let value = Int64(amountText.trimmingCharacters(in: .whitespacesAndNewlines)), value > 0 else {
            return nil
        }
        return value
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

    private func createDeposit() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing Privy access token."
            return
        }
        guard let amount = parsedAmount else {
            errorMessage = "Enter a positive amount."
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
