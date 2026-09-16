import SwiftUI

/// Partial cash out with slider; full exit is slider at max.
struct RedeemView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let maxShareUnits: Int64
    let memberWalletAddress: String
    var onRedeemSuccess: () async -> Void = {}

    private let apiClient = MonacoAPIClient()
    private let dustMinimumMicros: Int64 = 1_000_000

    @State private var sliderValue: Double = 0
    @State private var payoutAddress = ""
    @State private var errorMessage: String?
    @State private var isSubmitting = false
    @State private var didRedeem = false

    var body: some View {
        Form {
            Section {
                Text("Cash out part of your slice. Slide to max for a full exit.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Section("Amount") {
                if maxShareUnits > 0 {
                    Slider(value: $sliderValue, in: 0...Double(maxShareUnits), step: 1)
                        .accessibilityIdentifier("redeem-slider")
                    Text("Share units: \(selectedShareUnits)")
                        .font(.caption.monospacedDigit())
                } else {
                    Text("No share units to cash out yet.")
                        .foregroundStyle(.secondary)
                }
            }

            Section("Payout address") {
                TextField("Solana USDC address", text: $payoutAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .font(.body.monospaced())
                    .accessibilityIdentifier("redeem-payout-address")
            }

            Section {
                Button(isSubmitting ? "Cashing out…" : "Cash out") {
                    Task { await submitRedeem() }
                }
                .disabled(isSubmitting || !canSubmit)
                .accessibilityIdentifier("redeem-submit-button")
            }

            if didRedeem {
                Section {
                    Label("Cash out started — boards will refresh.", systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                }
            }

            if let errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.orange)
                }
            }
        }
        .navigationTitle("Cash out")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var selectedShareUnits: Int64 {
        Int64(sliderValue.rounded())
    }

    private var canSubmit: Bool {
        guard selectedShareUnits > 0,
              !payoutAddress.trimmingCharacters(in: .whitespaces).isEmpty,
              !memberWalletAddress.isEmpty else {
            return false
        }
        return selectedShareUnits >= dustMinimumMicros
    }

    private func submitRedeem() async {
        guard let token = auth.accessToken, canSubmit else { return }
        isSubmitting = true
        errorMessage = nil
        defer { isSubmitting = false }

        let proof = PayoutProofCollector.collectProof(
            payoutAddress: payoutAddress.trimmingCharacters(in: .whitespaces),
            memberWalletAddress: memberWalletAddress
        )

        do {
            _ = try await apiClient.postRedeem(
                accessToken: token,
                groupId: groupId,
                shareUnits: String(selectedShareUnits),
                payoutAddress: payoutAddress.trimmingCharacters(in: .whitespaces),
                payoutProof: proof
            )
            let refresh = RedeemBoardRefreshCoordinator(apiClient: apiClient)
            try await refresh.refreshAfterSuccess(accessToken: token, groupId: groupId)
            didRedeem = true
            await onRedeemSuccess()
        } catch MonacoAPIError.httpStatus(let code) {
            errorMessage = "Cash out failed (HTTP \(code))."
        } catch {
            errorMessage = "Could not complete cash out."
        }
    }
}
