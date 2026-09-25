import MonacoCore
import SwiftUI
import UIKit

/// Add money: the member's USDC deposit address on Solana. What lands there becomes account
/// balance; funding a cabal is its own step.
///
/// Owns the address load, the balance poll and the toasts. `DepositContent` is the layout.
struct DepositView: View {
    @ObservedObject var auth: PrivyAuthService
    var joinedCabals: [HomeGroupBoardRowDTO] = []
    var preselectedGroupId: String?

    @Environment(AppSessionStore.self) private var session

    private let apiClient = MonacoAPIClient()

    @State private var depositAddress: String?
    @State private var errorMessage: String?
    @State private var isLoading = true
    /// The balance this screen last announced, so the arrival toast does not depend on a shared
    /// store that other screens also write.
    @State private var lastAnnouncedBalanceMicros: Int64?
    @State private var toast: MonacoToast?

    var body: some View {
        DepositContent(
            auth: auth,
            address: .resolve(isLoading: isLoading, address: depositAddress, errorMessage: errorMessage),
            balance: .resolve(balance: session.platformBalance, isLoading: session.isBalanceLoading),
            pendingAllocationMicros: session.platformBalance?.pendingAllocationMicros ?? 0,
            joinedCabals: joinedCabals,
            preselectedGroupId: preselectedGroupId,
            onCopy: copyAddress,
            onRetry: { Task { await loadDepositAddress() } }
        )
        .monacoToast($toast)
        .task(id: auth.accessToken) {
            await loadDepositAddress()
        }
        .pollWhileVisible(every: DepositPolling.balanceInterval, isActive: depositAddress != nil) {
            try await refreshPlatformBalance()
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
        if let known = DepositAddress.usable(session.me?.memberWalletAddress) {
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
            // the member wallet exists (backend SessionService.OpenSession); `GET /v1/me` answers
            // 404 without it. The store being empty means the shell has not got that far, so this
            // screen has to do it rather than show "not ready yet" to a brand-new member.
            let profile = try await apiClient.openSession(accessToken: accessToken)
            guard let address = DepositAddress.usable(profile.memberWalletAddress) else {
                errorMessage = "Deposit address not ready yet."
                isLoading = false
                return
            }
            session.me = profile
            depositAddress = address
        } catch MonacoAPIError.httpStatus {
            // The Try again button on the address card is the way back, so the copy does not send
            // the member pulling on a screen that has no pull-to-refresh.
            errorMessage = "Couldn't load your deposit address."
        } catch where error.isRequestCancellation {
            // The hourly token rotation restarts `.task(id: auth.accessToken)` and cancels this
            // request. Nothing went wrong, so nothing is claimed about the connection — but the
            // skeleton is not left running either: a cancellation is not proof that a replacement
            // is on its way (URLSession reports -999 for more than a cancelled task), and the
            // card's Try again button is the member's way out of any state but the skeleton.
            errorMessage = nil
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
        if session.platformBalance != fresh {
            session.platformBalance = fresh
        }

        // What this screen has announced, kept locally. The shared store is written by the shell
        // and by Home as well, so comparing against it meant a refresh of theirs landing first
        // made the number "unchanged" here and swallowed the arrival toast entirely. The first
        // tick only seeds: arriving on a screen is not money arriving.
        let previous = lastAnnouncedBalanceMicros
        lastAnnouncedBalanceMicros = fresh.availableUsdcMicros
        guard let previous, fresh.availableUsdcMicros > previous else { return }
        toast = MonacoToast(message: "USDC arrived in your account balance.", isSuccess: true)
    }
}

/// Add money's layout, top to bottom in the order a member uses it: the address to copy (the
/// one card on the screen, because it is the one thing to act on), the balance it fills with
/// the way on to a cabal under it, and how the whole thing works as three ruled lines.
///
/// Pure: what the screen knows comes in, what the member does goes out.
struct DepositContent: View {
    @ObservedObject var auth: PrivyAuthService
    let address: DepositAddressCard.Content
    let balance: HomeBalanceDisplay
    var pendingAllocationMicros: Int64 = 0
    let joinedCabals: [HomeGroupBoardRowDTO]
    var preselectedGroupId: String?
    let onCopy: (String) -> Void
    let onRetry: () -> Void

    static let steps = [
        "Send USDC to the address above from an exchange or another app.",
        "Your account balance updates a few seconds after it arrives.",
        "Fund a cabal to move it into the pot and grow your slice.",
    ]

    var body: some View {
        ScrollView {
            // No horizontal padding on the stack: the ruled lists run edge to edge, and the card
            // and each header inset themselves.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                DepositAddressCard(content: address, onCopy: onCopy, onRetry: onRetry)
                    .padding(.horizontal, MonacoTheme.Space.m)

                balanceSection

                howItWorks
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle("Add money")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// The balance the address fills, with the next step under it once there is a cabal to
    /// fund — Home's balance row, with Home's text action.
    private var balanceSection: some View {
        MonacoGroupedList {
            PlatformBalanceCard(
                display: balance,
                pendingAllocationMicros: pendingAllocationMicros,
                valueIdentifier: "deposit-screen-balance-value"
            )

            if !joinedCabals.isEmpty {
                NavigationLink {
                    FundCabalView(
                        auth: auth,
                        joinedCabals: joinedCabals,
                        preselectedGroupId: preselectedGroupId
                    )
                } label: {
                    Text("Fund a cabal")
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.brand)
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("deposit-fund-cabal-link")
                .padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
                .padding(.trailing, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.xs)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private var howItWorks: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("How it works")
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                ForEach(Array(Self.steps.enumerated()), id: \.offset) { index, step in
                    DepositStepRow(number: index + 1, text: step, isLast: index == Self.steps.count - 1)
                }
            }
        }
    }
}

/// One step of "How it works": the number in the market's voice, the sentence in the brand's.
private struct DepositStepRow: View {
    let number: Int
    let text: String
    let isLast: Bool

    @ScaledMetric(relativeTo: .subheadline) private var numberColumn: CGFloat = 20

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.sm) {
            Text("\(number)")
                .font(MonacoTheme.Typo.data)
                .foregroundStyle(MonacoTheme.tertiaryText)
                .frame(width: numberColumn, alignment: .leading)
            Text(text)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 52)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule()
                    .padding(.leading, MonacoTheme.Space.m + numberColumn + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Step \(number). \(text)")
    }
}

/// The deposit address as the one card on a money screen: it is the thing the member acts on.
/// The address in the market's voice, never hyphenated; a full-width Copy; and the one rule
/// that matters — Solana only — as a caption under it rather than a warning.
///
/// Add money shows every state of it. Fund this cabal shows it when there is nothing to fund
/// with yet, under its own identifiers.
struct DepositAddressCard: View {
    enum Content: Equatable {
        case loading
        case ready(String)
        /// Why there is no address, in the member's words.
        case unavailable(String)

        /// The address load's three values as one state: a load in flight wins, then an
        /// address, then whatever went wrong — "not ready yet" when nothing was said.
        static func resolve(isLoading: Bool, address: String?, errorMessage: String?) -> Content {
            if isLoading { return .loading }
            if let address { return .ready(address) }
            return .unavailable(errorMessage ?? "Deposit address not ready yet.")
        }
    }

    let content: Content
    var addressIdentifier = "deposit-address-value"
    var copyIdentifier = "deposit-address-copy-button"
    let onCopy: (String) -> Void
    /// Offered on `unavailable` when set.
    var onRetry: (() -> Void)?

    static let networkNote = "Send USDC on the Solana network only."

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            Text("Your deposit address")
                .font(MonacoTheme.Typo.captionStrong)
                .foregroundStyle(MonacoTheme.muted)

            switch content {
            case .loading:
                loading
            case .ready(let address):
                ready(address)
            case .unavailable(let message):
                unavailable(message)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .monacoSurfaceCard()
    }

    private func ready(_ address: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoWalletAddressText(address: address)
                .accessibilityIdentifier(addressIdentifier)
                .onTapGesture {
                    onCopy(address)
                }

            VStack(spacing: MonacoTheme.Space.s) {
                Button("Copy address") {
                    onCopy(address)
                }
                .buttonStyle(.monacoPrimary)
                .monacoFullWidthButtons()
                .accessibilityIdentifier(copyIdentifier)

                Text(Self.networkNote)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
            }
        }
    }

    /// The card's own shape while the address loads: two lines of address and the button.
    private var loading: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                SkeletonBlock(height: 16)
                SkeletonBlock(width: 120, height: 16)
            }
            SkeletonBlock(height: MonacoButtonMetrics.minimumHeight, radius: MonacoButtonMetrics.minimumHeight / 2)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading your deposit address")
        .accessibilityIdentifier("deposit-address-loading")
    }

    private func unavailable(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            Text(message)
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            if let onRetry {
                Button("Try again", action: onRetry)
                    .buttonStyle(.monacoSecondary)
                    .monacoFullWidthButtons()
                    .accessibilityIdentifier("deposit-address-retry")
            }
        }
    }
}

#Preview {
    NavigationStack {
        DepositView(auth: PrivyAuthService())
    }
}
