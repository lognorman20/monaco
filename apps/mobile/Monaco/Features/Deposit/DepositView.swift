import SwiftUI
import UIKit

/// Add money — show member deposit address; backend sweep credits club share.
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String

    private let apiClient = MonacoAPIClient()

    @State private var depositAddress: String?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var didCopy = false

    var body: some View {
        Form {
            Section {
                Text("Send USDC on Solana to your deposit address. We sweep it into your club's vault and credit your share when it lands.")
                    .monacoSecondaryCaption()
            }

            Section("Your deposit address") {
                if isLoading {
                    HStack(spacing: 12) {
                        ProgressView()
                            .tint(MonacoTheme.accent)
                        Text("Loading address…")
                            .monacoSecondaryCaption()
                    }
                    .accessibilityIdentifier("deposit-address-loading")
                } else if let depositAddress {
                    addressBlock(depositAddress)
                } else {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(
                            errorMessage ?? "Deposit address not ready yet.",
                            systemImage: "exclamationmark.triangle.fill"
                        )
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.warning)

                        Button("Try again") {
                            Task { await loadDepositAddress() }
                        }
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("deposit-address-retry")
                    }
                }
            }

            Section("How it works") {
                stepRow(number: 1, text: "Send USDC on Solana to the address above.")
                stepRow(number: 2, text: "Monaco sweeps your deposit into the club vault.")
                stepRow(number: 3, text: "Your share in the pot updates once the sweep completes.")
            }
        }
        .monacoFormScreen()
        .navigationTitle("Add money")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: auth.accessToken) {
            await loadDepositAddress()
        }
    }

    @ViewBuilder
    private func addressBlock(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            MonacoWalletAddressText(address: address)
                .accessibilityIdentifier("deposit-address-value")
                .onTapGesture {
                    copyAddress(address)
                }

            HStack {
                Button {
                    copyAddress(address)
                } label: {
                    Label(didCopy ? "Copied" : "Copy address", systemImage: didCopy ? "checkmark" : "doc.on.doc")
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("deposit-address-copy-button")

                if didCopy {
                    Text("Copied")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(MonacoTheme.success)
                        .accessibilityIdentifier("deposit-address-copied-feedback")
                }
            }
        }
    }

    private func stepRow(number: Int, text: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Text("\(number).")
                .font(.subheadline.weight(.semibold).monospacedDigit())
                .foregroundStyle(MonacoTheme.accent)
                .frame(width: 20, alignment: .trailing)
            Text(text)
                .font(.subheadline)
                .foregroundStyle(MonacoTheme.primaryText)
        }
    }

    private func copyAddress(_ address: String) {
        UIPasteboard.general.string = address
        didCopy = true
        Task {
            try? await Task.sleep(for: .seconds(2))
            didCopy = false
        }
    }

    private func loadDepositAddress() async {
        guard let accessToken = auth.accessToken else {
            depositAddress = nil
            errorMessage = "Sign in to view your deposit address."
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        depositAddress = nil

        do {
            _ = try await apiClient.openSession(accessToken: accessToken)
            let profile = try await apiClient.me(accessToken: accessToken)
            let address = profile.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !address.isEmpty, !address.hasPrefix("FAKE") else {
                errorMessage = "Deposit address not ready yet."
                isLoading = false
                return
            }
            depositAddress = address
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not load address (HTTP \(status))."
        } catch {
            errorMessage = "Could not load deposit address."
        }

        isLoading = false
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: PrivyAuthService(), groupId: "00000000-0000-0000-0000-000000000001")
    }
}
