import SwiftUI
import UIKit

/// App settings — account wallet and withdraw.
struct SettingsView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var memberWalletAddress: String?
    @State private var isLoadingAddress = true
    @State private var addressError: String?
    @State private var toast: MonacoToast?

    var body: some View {
        Form {
            Section("Your deposit address") {
                if isLoadingAddress {
                    HStack(spacing: 12) {
                        ProgressView().tint(MonacoTheme.accent)
                        Text("Loading address…")
                            .monacoSecondaryCaption()
                    }
                    .accessibilityIdentifier("settings-deposit-address-loading")
                } else if let memberWalletAddress {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Send USDC on Solana here to add to your account balance.")
                            .monacoSecondaryCaption()
                        MonacoWalletAddressText(address: memberWalletAddress)
                            .accessibilityIdentifier("settings-deposit-address-value")
                            .onTapGesture { copyAddress(memberWalletAddress) }
                        Button {
                            copyAddress(memberWalletAddress)
                        } label: {
                            Label("Copy address", systemImage: "doc.on.doc")
                        }
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("settings-deposit-address-copy")
                    }
                } else {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(addressError ?? "Deposit address not ready yet.")
                            .foregroundStyle(MonacoTheme.warning)
                        Button("Try again") {
                            Task { await loadDepositAddress() }
                        }
                        .monacoFormSecondaryAction()
                        .accessibilityIdentifier("settings-deposit-address-retry")
                    }
                }
            }

            Section {
                NavigationLink {
                    WithdrawView(auth: auth)
                } label: {
                    Label("Withdraw", systemImage: "arrow.up.right")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("settings-withdraw-link")

                NavigationLink {
                    AdvancedSettingsView()
                } label: {
                    Label("Advanced", systemImage: "link")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("settings-advanced-link")
            }

            Section {
                Button("Sign out") {
                    Task { await auth.logout() }
                }
                .monacoFormDestructiveAction()
                .accessibilityIdentifier("settings-sign-out")
            }
        }
        .monacoFormScreen()
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .task(id: auth.accessToken) {
            await loadDepositAddress()
        }
    }

    private func copyAddress(_ address: String) {
        UIPasteboard.general.string = address
        toast = MonacoToast(message: "Address copied.", isSuccess: true)
    }

    private func loadDepositAddress() async {
        guard let token = auth.accessToken else {
            memberWalletAddress = nil
            addressError = "Sign in to view your deposit address."
            isLoadingAddress = false
            return
        }

        isLoadingAddress = true
        addressError = nil

        do {
            if let balance = try? await apiClient.getPlatformBalance(accessToken: token) {
                let fromBalance = balance.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines)
                if !fromBalance.isEmpty, !fromBalance.hasPrefix("FAKE") {
                    memberWalletAddress = fromBalance
                    isLoadingAddress = false
                    return
                }
            }

            _ = try await apiClient.openSession(accessToken: token)
            let profile = try await apiClient.me(accessToken: token)
            let address = profile.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !address.isEmpty, !address.hasPrefix("FAKE") else {
                memberWalletAddress = nil
                addressError = "Deposit address not ready yet."
                isLoadingAddress = false
                return
            }
            memberWalletAddress = address
        } catch MonacoAPIError.httpStatus(let status) {
            memberWalletAddress = nil
            addressError = "Could not load address (HTTP \(status))."
        } catch {
            memberWalletAddress = nil
            addressError = "Could not load deposit address."
        }

        isLoadingAddress = false
    }
}

struct AdvancedSettingsView: View {
    var body: some View {
        Form {
            Section("Block explorers") {
                ForEach(SettingsAdvancedLinks.explorerLinks) { link in
                    Link(destination: link.url) {
                        Label(link.title, systemImage: "safari")
                            .foregroundStyle(MonacoTheme.accent)
                    }
                    .accessibilityIdentifier("settings-explorer-\(link.id)")
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview {
    NavigationStack {
        SettingsView(auth: PrivyAuthService())
    }
}
