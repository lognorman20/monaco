import MonacoCore
import SwiftUI
import UIKit

/// Move USDC from account balance into a joined cabal's pot.
struct FundCabalView: View {
    @ObservedObject var auth: PrivyAuthService
    let joinedCabals: [HomeGroupBoardRowDTO]
    var preselectedGroupId: String?
    var onFunded: () async -> Void = {}

    private let apiClient = MonacoAPIClient()

    @StateObject private var balanceLoader = PlatformBalanceLoader()
    @State private var selectedGroupId: String?
    @State private var amountText = ""
    @State private var isSubmitting = false
    /// Idempotency key for the fund request in flight; a retry after a lost response reuses it.
    @State private var fundSubmission = IdempotentSubmission()
    /// The fund whose sweep this screen is watching, if any.
    @State private var sweep: FundSweep?
    @State private var toast: MonacoToast?

    /// One fund on its way into the pot, kept as a value so `.task(id:)` owns the watching.
    private struct FundSweep: Equatable {
        let depositId: String
        let cabalName: String
        let amountLabel: String
    }

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
        .task(id: sweep) {
            guard let sweep else { return }
            await watchFundSweep(sweep)
        }
        .pollWhileVisible(every: DepositPolling.balanceInterval, isActive: auth.accessToken != nil) {
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
            Text("Send USDC on Solana to your deposit address. Your account balance updates when it arrives, then you can fund this cabal.")
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

        let fundedAmountLabel = AmountEntryText.display(amountText)
        do {
            let fund = try await apiClient.fundGroup(accessToken: token, groupId: groupId, amount: micros, submission: fundSubmission)
            Haptics.success()
            let name = selectedCabalName ?? "your cabal"
            toast = MonacoToast(message: "Adding \(fundedAmountLabel) to \(name)…", isSuccess: true)
            amountText = ""
            // A reload here leaves the amount pad and the button exactly where they are: the
            // loader keeps the balance on screen while it refreshes.
            await balanceLoader.load(accessToken: token)
            await onFunded()
            sweep = FundSweep(depositId: fund.depositId, cabalName: name, amountLabel: fundedAmountLabel)
        } catch {
            if error.isRequestCancellation { return }
            toast = MonacoToast(message: MoneyFlowCopy.fundCabalFailure(FlowErrorInput(error)).summary, isSuccess: false)
        }
    }

    /// Watches one fund until the pot has it. Structured, so it stops with the screen instead of
    /// polling on for two minutes to post a toast nobody is there to read — Activity on the cabal
    /// carries the outcome either way.
    private func watchFundSweep(_ sweep: FundSweep) async {
        guard let token = auth.accessToken else { return }
        var machine = DepositPollStateMachine()
        let phase = await machine.pollUntilTerminal {
            let deposit = try await apiClient.getDeposit(accessToken: token, depositId: sweep.depositId)
            return deposit.status
        }
        guard !Task.isCancelled else { return }
        switch phase {
        case .credited:
            toast = MonacoToast(message: "Added \(sweep.amountLabel) to \(sweep.cabalName).", isSuccess: true)
            await onFunded()
        case .failed:
            toast = MonacoToast(message: "Couldn't add money to the cabal. Try again.", isSuccess: false)
            await onFunded()
        case .idle, .awaitingSweep:
            toast = MonacoToast(
                message: "Still adding \(sweep.amountLabel) to \(sweep.cabalName). Check activity for updates.",
                isSuccess: true
            )
        }

        // This fund has been watched to its end and spoken for. Forgetting it stops `.task(id:)`
        // from starting the watch again on the next re-appear — switching tabs tears this task
        // down, and coming back would otherwise re-poll a deposit that already landed and toast
        // money from a previous session as if it had just arrived.
        self.sweep = nil
    }
}
