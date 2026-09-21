import MonacoCore
import SwiftUI
import UIKit

/// Deposit — inbound USDC lands in account balance; fund a cabal separately.
struct DepositView: View {
    @ObservedObject var auth: DynamicAuthService
    var joinedCabals: [HomeGroupBoardRowDTO] = []
    var preselectedGroupId: String?

    /// Optional so previews and sample harnesses without the shell still render.
    @Environment(AppSessionStore.self) private var session: AppSessionStore?

    private let apiClient = MonacoAPIClient()

    @State private var depositAddress: String?
    @State private var errorMessage: String?
    @State private var isLoading = true
    /// The balance this screen last announced, so the arrival toast does not depend on a shared
    /// store that other screens also write.
    @State private var lastAnnouncedBalanceMicros: Int64?
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
        .pollWhileVisible(every: AddMoneyPolling.balanceInterval, isActive: depositAddress != nil) {
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
        // Signed out first: `session.me` outlives the token, so reading the store before checking
        // for one would show a signed-out member a deposit address from a stale profile.
        guard let accessToken = auth.accessToken else {
            depositAddress = nil
            errorMessage = "Sign in to view your deposit address."
            isLoading = false
            return
        }

        // The shell opened the backend session and read the profile before this screen existed,
        // so the address is already in hand. Two more round trips to fetch it again only kept the
        // member on a spinner.
        if let known = DepositAddress.usable(session?.me?.memberWalletAddress) {
            depositAddress = known
            errorMessage = nil
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        depositAddress = nil

        do {
            // Only on this path. `POST /v1/auth/session` is what upserts the user row and ensures
            // the member wallet exists (backend SessionService.OpenSession → EnsureMemberWallet);
            // `GET /v1/me` answers 404 without it. The store being empty means the shell has not
            // got that far, so this screen has to do it rather than tell a brand-new member the
            // address is not ready. It returns the same profile, so one call is enough.
            let profile = try await apiClient.openSession(accessToken: accessToken)
            guard let address = DepositAddress.usable(profile.memberWalletAddress) else {
                errorMessage = "Deposit address not ready yet."
                isLoading = false
                return
            }
            session?.me = profile
            depositAddress = address
        } catch MonacoAPIError.httpStatus {
            // The Try again button in this section is the way back, so the copy does not send the
            // member pulling on a screen that has no pull-to-refresh.
            errorMessage = "Couldn't load your deposit address."
        } catch where error.isRequestCancellation {
            // The token rotation restarts `.task(id: auth.accessToken)` and cancels this request.
            // Nothing went wrong, so nothing is claimed about the connection — but the spinner is
            // not left running either: a cancellation is not proof that a replacement is on its
            // way (URLSession reports -999 for more than a cancelled task), and the section's
            // Try again button is the member's way out of any state but the spinner.
            errorMessage = nil
        } catch {
            errorMessage = "No connection. Check your internet and try again."
        }

        isLoading = false
    }

    /// Refreshes the account balance while the deposit screen is open so inbound USDC shows
    /// quickly. Writes the shared store only when the number actually moved, so the tabs reading
    /// it are not re-rendered every few seconds for nothing.
    private func refreshPlatformBalance() async throws {
        guard let token = auth.accessToken else { return }
        let fresh = try await apiClient.getPlatformBalance(accessToken: token)
        if let session, session.platformBalance != fresh {
            session.platformBalance = fresh
        }

        // What this screen has announced, kept locally. The shared store is written by the shell
        // and by Home as well, so comparing against it would let a refresh of theirs landing
        // first make the number look unchanged here and swallow the arrival toast. The first
        // tick only seeds: arriving on a screen is not money arriving.
        let previous = lastAnnouncedBalanceMicros
        lastAnnouncedBalanceMicros = fresh.availableUsdcMicros
        guard let previous, fresh.availableUsdcMicros > previous else { return }
        toast = MonacoToast(message: "USDC arrived in your account balance.", isSuccess: true)
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: DynamicAuthService())
    }
}
