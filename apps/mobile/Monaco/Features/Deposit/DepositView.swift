import MonacoCore
import SwiftUI
import UIKit

/// Deposit — inbound USDC lands in account balance; fund a cabal separately.
struct DepositView: View {
    @ObservedObject var auth: DynamicAuthService
    var joinedCabals: [HomeGroupBoardRowDTO] = []
    var preselectedGroupId: String?

    private let apiClient = MonacoAPIClient()

    @State private var depositAddress: String?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var toast: MonacoToast?

    var body: some View {
        Form {
            Section {
                Text("Send USDC on Base to this address")
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
                stepRow(number: 1, text: "Send USDC on Base to this address")
                stepRow(number: 2, text: "Your account balance updates when USDC arrives.")
                stepRow(number: 3, text: "Fund a cabal to move USDC into the pot and credit your share.")
            }

            if !joinedCabals.isEmpty {
                Section("Fund a cabal") {
                    NavigationLink {
                        FundCabalView(
                            auth: auth,
                            joinedCabals: joinedCabals,
                            preselectedGroupId: preselectedGroupId
                        )
                    } label: {
                        Label("Choose cabal and amount", systemImage: "arrow.right.circle")
                    }
                    .accessibilityIdentifier("deposit-fund-cabal-link")
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Add money")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
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

            Button {
                copyAddress(address)
            } label: {
                Label("Copy address", systemImage: "doc.on.doc")
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("deposit-address-copy-button")
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
        toast = MonacoToast(message: "Address copied.", isSuccess: true)
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
        } catch MonacoAPIError.httpStatus {
            // The Try again button in this section is the way back, so the copy does not send the
            // member pulling on a screen that has no pull-to-refresh.
            errorMessage = "Couldn't load your deposit address."
        } catch {
            errorMessage = "No connection. Check your internet and try again."
        }

        isLoading = false
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: DynamicAuthService())
    }
}
