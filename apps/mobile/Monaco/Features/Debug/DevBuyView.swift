import SwiftUI

/// M3 debug control: POST /v1/dev/groups/{id}/buy for stub AAPLx buy (backend-only Jupiter).
struct DevBuyView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    /// 0.1 USDC in micro-units — small dev stub amount.
    private static let devBuyUSDCMicro: Int64 = 100_000

    @State private var result: DevBuyResponse?
    @State private var errorMessage: String?
    @State private var isSubmitting = false

    var body: some View {
        Form {
            Section("Dev buy (M3 stub)") {
                Text("Triggers backend POST /v1/dev/groups/{id}/buy. No Jupiter or xStocks from Swift.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Section("Group") {
                Text(groupId)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
            }

            Section("Order") {
                detailRow(title: "Symbol", value: "AAPLx")
                detailRow(title: "USDC", value: "0.1 USDC (\(Self.devBuyUSDCMicro) micro-units)")
            }

            Section {
                Button(isSubmitting ? "Buying…" : "Buy AAPLx (dev)") {
                    Task { await executeDevBuy() }
                }
                .disabled(isSubmitting || auth.accessToken == nil)
                .accessibilityIdentifier("dev-buy-aaplx-button")
            }

            if let result {
                Section("Result") {
                    detailRow(title: "Transaction ID", value: result.transactionId)
                    detailRow(title: "Status", value: result.status)
                    detailRow(title: "Symbol", value: result.symbol)
                    detailRow(title: "Created", value: result.created ? "yes" : "no (idempotent)")
                    if let txSignature = result.txSignature, !txSignature.isEmpty {
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
        .navigationTitle("Dev buy")
        .navigationBarTitleDisplayMode(.inline)
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

    private func executeDevBuy() async {
        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing Privy access token."
            return
        }

        isSubmitting = true
        errorMessage = nil
        result = nil

        do {
            let response = try await apiClient.devBuy(
                accessToken: accessToken,
                groupId: groupId,
                symbol: "AAPLx",
                usdc: Self.devBuyUSDCMicro
            )
            result = response
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Dev buy failed (HTTP \(status))."
        } catch {
            errorMessage = "Could not execute dev buy."
        }

        isSubmitting = false
    }
}

#Preview {
    NavigationStack {
        DevBuyView(auth: PrivyAuthService(), groupId: "00000000-0000-0000-0000-000000000001")
    }
}
