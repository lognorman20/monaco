import MonacoCore
import SwiftUI
import UIKit

/// Deposit — inbound USDC lands in account balance; fund a cabal separately.
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    var joinedCabals: [HomeGroupBoardRowDTO] = []
    var preselectedGroupId: String?

    @Environment(AppSessionStore.self) private var session

    private let apiClient = MonacoAPIClient()

    @State private var depositAddress: String?
    @State private var errorMessage: String?
    @State private var isLoading = true
    @State private var toast: MonacoToast?

    var body: some View {
        Form {
            Section {
                Text("Send USDC on the Solana network only. Your account balance updates within a few seconds after it lands on chain.")
                    .monacoSecondaryCaption()
            }

            if let balance = session.platformBalance {
                Section("Account balance") {
                    MoneyText(micros: balance.availableUsdcMicros, style: .row)
                        .accessibilityIdentifier("deposit-screen-balance-value")
                }
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
        .pollWhileVisible(every: DepositPolling.balanceInterval, isActive: depositAddress != nil) {
            try await refreshPlatformBalance()
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
        // The shell opened the backend session and read the profile before this screen existed,
        // so the address is already in hand. Opening a second session and asking for the profile
        // again only kept the member on a spinner.
        if let known = DepositAddress.usable(session.me?.memberWalletAddress) {
            depositAddress = known
            errorMessage = nil
            isLoading = false
            return
        }

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
            let profile = try await apiClient.me(accessToken: accessToken)
            guard let address = DepositAddress.usable(profile.memberWalletAddress) else {
                errorMessage = "Deposit address not ready yet."
                isLoading = false
                return
            }
            session.me = profile
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

    /// Refreshes the account balance while the deposit screen is open so inbound USDC shows
    /// quickly. Writes the shared store only when the number actually moved, so the tabs reading
    /// it are not re-rendered every three seconds for nothing.
    private func refreshPlatformBalance() async throws {
        guard let token = auth.accessToken else { return }
        let fresh = try await apiClient.getPlatformBalance(accessToken: token)
        let previous = session.platformBalance
        guard fresh != previous else { return }
        session.platformBalance = fresh
        if let previous, fresh.availableUsdcMicros > previous.availableUsdcMicros {
            toast = MonacoToast(message: "USDC arrived in your account balance.", isSuccess: true)
        }
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: PrivyAuthService())
    }
}
