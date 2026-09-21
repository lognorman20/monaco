import MonacoCore
import SwiftUI
import UIKit

/// Move USDC from account balance into a joined cabal's pot.
struct FundCabalView: View {
    @ObservedObject var auth: DynamicAuthService
    let joinedCabals: [HomeGroupBoardRowDTO]
    var preselectedGroupId: String?
    var onFunded: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @StateObject private var balanceLoader = PlatformBalanceLoader()
    @State private var selectedGroupId: String?
    @State private var amountText = ""
    @State private var isSubmitting = false
    @State private var toast: MonacoToast?

    private var isSingleCabalContext: Bool {
        preselectedGroupId != nil
    }

    private var selectedCabalName: String? {
        joinedCabals.first(where: { $0.groupId == selectedGroupId })?.name
    }

    private var balance: PlatformBalanceDTO? {
        balanceLoader.balance
    }

    private var maxDollars: Decimal? {
        guard let micros = balance?.availableUsdcMicros, micros > 0 else { return nil }
        return Decimal(micros) / Decimal(1_000_000)
    }

    private var hasNothingToFundWith: Bool {
        guard let balance else { return false }
        return balance.availableUsdcMicros <= 0
    }

    /// The amount pad and its button belong together: whenever one is on screen, so is the other.
    private var showsAmountEntry: Bool {
        guard case .loaded = balanceLoader.phase else { return false }
        return !joinedCabals.isEmpty && !hasNothingToFundWith
    }

    private var ctaTitle: String {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return "Add money" }
        return "Add \(AmountEntryText.display(amountText)) to the pot"
    }

    private var canSubmit: Bool {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return false }
        if let maxDollars { return value <= maxDollars }
        return false
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                if !isSingleCabalContext, !joinedCabals.isEmpty {
                    cabalPicker
                }

                switch balanceLoader.phase {
                case .loading:
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.xl)
                        .accessibilityIdentifier("fund-cabal-loading")
                case .failed(let message):
                    balanceUnavailable(message)
                case .loaded(let balance):
                    PlatformBalanceCard(balance: balance)
                    if joinedCabals.isEmpty {
                        Text("Join a cabal first, then fund it from your account balance.")
                            .font(MonacoTheme.Typo.body)
                            .foregroundStyle(MonacoTheme.muted)
                    } else if hasNothingToFundWith {
                        depositPrompt
                    } else {
                        AmountEntry(
                            amountText: $amountText,
                            max: maxDollars,
                            presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                            helper: "From your account balance",
                            showsKeyboardDoneButton: true
                        )
                        .padding(.top, MonacoTheme.Space.l)
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if showsAmountEntry {
                BottomCTA {
                    Button(isSubmitting ? "Adding money…" : ctaTitle) {
                        Task { await submitFund() }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSubmitting || selectedGroupId == nil || !canSubmit)
                    .accessibilityIdentifier("fund-cabal-submit-button")
                }
            }
        }
        .navigationTitle("Add money")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("fund-cabal-view")
        .monacoToast($toast, bottomInset: 72)
        .task(id: auth.accessToken) {
            if selectedGroupId == nil {
                selectedGroupId = preselectedGroupId ?? joinedCabals.first?.groupId
            }
            await balanceLoader.load(accessToken: auth.accessToken)
        }
        .pollWhileVisible(every: AddMoneyPolling.balanceInterval, isActive: auth.accessToken != nil) {
            try await refreshBalance()
        }
    }

    private var cabalPicker: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Cabal")
            Picker("Cabal", selection: $selectedGroupId) {
                ForEach(joinedCabals) { cabal in
                    Text(cabal.name).tag(Optional(cabal.groupId))
                }
            }
            .pickerStyle(.menu)
            .tint(MonacoTheme.ink)
            .accessibilityIdentifier("fund-cabal-picker")
        }
    }

    private func balanceUnavailable(_ message: String) -> some View {
        EmptyState(
            title: "Balance unavailable",
            message: message,
            actionTitle: "Try again",
            action: { Task { await balanceLoader.load(accessToken: auth.accessToken) } }
        )
        .padding(.top, MonacoTheme.Space.xl)
        .accessibilityIdentifier("fund-cabal-balance-error")
    }

    private var depositPrompt: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Add USDC first")
            Text("Send USDC on Base to this address. Your account balance updates when it arrives, then you can fund this cabal.")
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
            if let address = validDepositAddress {
                MonacoWalletAddressText(address: address)
                    .accessibilityIdentifier("fund-cabal-deposit-address")
                Button {
                    copyAddress(address)
                } label: {
                    Text("Copy deposit address")
                }
                .buttonStyle(.monacoSecondary)
                .accessibilityIdentifier("fund-cabal-copy-deposit-address")
            } else {
                Text("Deposit address not ready yet.")
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.warning)
            }
        }
    }

    private var validDepositAddress: String? {
        DepositAddress.usable(balance?.memberWalletAddress)
    }

    private func copyAddress(_ address: String) {
        UIPasteboard.general.string = address
        toast = MonacoToast(message: "Address copied.", isSuccess: true)
    }

    /// The background poll. It never shows a spinner or an error; it only announces money arriving.
    private func refreshBalance() async throws {
        let hadNothing = (balance?.availableUsdcMicros ?? 0) == 0
        guard let fresh = try await balanceLoader.refresh(accessToken: auth.accessToken) else { return }
        if hadNothing, fresh.availableUsdcMicros > 0 {
            toast = MonacoToast(message: "USDC arrived. You can fund your cabal now.", isSuccess: true)
        }
    }

    private func submitFund() async {
        // The disabled state only lands on the next render; a second tap in the same frame
        // must not fund the pot twice.
        guard !isSubmitting else { return }
        guard let token = auth.accessToken else { return }
        guard let groupId = selectedGroupId else { return }
        guard let value = AmountEntryText.decimal(amountText), value > 0, let micros = AmountEntryText.micros(amountText) else {
            toast = MonacoToast(message: "Enter a valid amount.", isSuccess: false)
            return
        }
        if let available = balance?.availableUsdcMicros, micros > available {
            toast = MonacoToast(message: "More than you have. Try a smaller amount.", isSuccess: false)
            return
        }

        isSubmitting = true
        defer { isSubmitting = false }

        do {
            _ = try await apiClient.fundGroup(accessToken: token, groupId: groupId, amount: micros)
            Haptics.success()
            let name = selectedCabalName ?? "your cabal"
            toast = MonacoToast(message: "Added \(AmountEntryText.display(amountText)) to \(name).", isSuccess: true)
            amountText = ""
            // A reload here leaves the amount pad and the button exactly where they are: the
            // loader keeps the balance on screen while it refreshes.
            await balanceLoader.load(accessToken: token)
            await onFunded()
        } catch {
            if error.isRequestCancellation { return }
            toast = MonacoToast(message: MoneyFlowCopy.fundCabalFailure(FlowErrorInput(error)).summary, isSuccess: false)
        }
    }
}
