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

    @State private var balance: PlatformBalanceDTO?
    @State private var selectedGroupId: String?
    @State private var amountText = ""
    @State private var isLoadingBalance = true
    @State private var isSubmitting = false
    @State private var errorMessage: String?
    @State private var toast: MonacoToast?

    private var isSingleCabalContext: Bool {
        preselectedGroupId != nil
    }

    private var selectedCabalName: String? {
        joinedCabals.first(where: { $0.groupId == selectedGroupId })?.name
    }

    private var screenTitle: String {
        guard let selectedCabalName else { return "Add money" }
        return "Add money to \(selectedCabalName)"
    }

    private var maxDollars: Decimal? {
        guard let micros = balance?.availableUsdcMicros, micros > 0 else { return nil }
        return Decimal(micros) / Decimal(1_000_000)
    }

    private var showDepositPrompt: Bool {
        guard let balance, !isLoadingBalance else { return false }
        return balance.availableUsdcMicros <= 0
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

                if let balance {
                    PlatformBalanceCard(balance: balance)
                }

                if isLoadingBalance {
                    ProgressView()
                        .tint(MonacoTheme.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, MonacoTheme.Space.xl)
                } else if joinedCabals.isEmpty {
                    Text("Join a cabal first, then fund it from your account balance.")
                        .font(MonacoTheme.Typo.body)
                        .foregroundStyle(MonacoTheme.muted)
                } else if showDepositPrompt {
                    depositPrompt
                } else {
                    AmountEntry(
                        amountText: $amountText,
                        max: maxDollars,
                        presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                        helper: "From your account balance"
                    )
                    .padding(.top, MonacoTheme.Space.l)
                }

                if let errorMessage, balance == nil, !isLoadingBalance {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.warning)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if !showDepositPrompt, !joinedCabals.isEmpty, !isLoadingBalance {
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
        .navigationTitle(screenTitle)
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast, bottomInset: 72)
        .task(id: auth.accessToken) {
            await loadBalance()
            if selectedGroupId == nil {
                selectedGroupId = preselectedGroupId ?? joinedCabals.first?.groupId
            }
            await pollBalanceWhileVisible()
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
        guard let address = balance?.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines),
              !address.isEmpty,
              !address.hasPrefix("FAKE") else {
            return nil
        }
        return address
    }

    private func copyAddress(_ address: String) {
        UIPasteboard.general.string = address
        toast = MonacoToast(message: "Address copied.", isSuccess: true)
    }

    private func loadBalance() async {
        guard let token = auth.accessToken else {
            balance = nil
            errorMessage = "Sign in to view your account balance."
            isLoadingBalance = false
            return
        }

        isLoadingBalance = true
        errorMessage = nil
        do {
            balance = try await apiClient.getPlatformBalance(accessToken: token)
        } catch MonacoAPIError.httpStatus {
            errorMessage = "Couldn't load your balance. Pull down to try again."
            balance = nil
        } catch {
            errorMessage = "No connection. Check your internet and try again."
            balance = nil
        }
        isLoadingBalance = false
    }

    private func submitFund() async {
        guard let token = auth.accessToken else { return }
        guard let groupId = selectedGroupId else { return }
        guard let value = AmountEntryText.decimal(amountText), value > 0 else {
            toast = MonacoToast(message: "Enter a valid amount.", isSuccess: false)
            return
        }
        var rounded = Decimal()
        var scaled = value * 1_000_000
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        let micros = (rounded as NSDecimalNumber).int64Value
        if let available = balance?.availableUsdcMicros, micros > available {
            toast = MonacoToast(message: "More than you have. Try a smaller amount.", isSuccess: false)
            return
        }

        isSubmitting = true
        defer { isSubmitting = false }

        let fundedAmountLabel = AmountEntryText.display(amountText)
        do {
            let fund = try await apiClient.fundGroup(accessToken: token, groupId: groupId, amount: micros)
            Haptics.success()
            let name = selectedCabalName ?? "your cabal"
            toast = MonacoToast(message: "Adding \(fundedAmountLabel) to \(name)…", isSuccess: true)
            amountText = ""
            await loadBalance()
            await onFunded()
            trackFundSweep(depositId: fund.depositId, cabalName: name, amountLabel: fundedAmountLabel)
        } catch MonacoAPIError.httpStatus(400) {
            toast = MonacoToast(message: "More than you have. Try a smaller amount.", isSuccess: false)
        } catch MonacoAPIError.httpStatus(403) {
            toast = MonacoToast(message: "You must be a cabal member to add money to it.", isSuccess: false)
        } catch MonacoAPIError.httpStatus {
            toast = MonacoToast(message: "Couldn't add that money. Try again.", isSuccess: false)
        } catch {
            toast = MonacoToast(message: "No connection. Check your internet and try again.", isSuccess: false)
        }
    }

    private func pollBalanceWhileVisible() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: DepositPolling.balanceInterval)
            guard !Task.isCancelled else { return }
            await refreshBalanceIfChanged()
        }
    }

    private func refreshBalanceIfChanged() async {
        guard let token = auth.accessToken else { return }
        guard let fresh = try? await apiClient.getPlatformBalance(accessToken: token) else { return }
        let previous = balance?.availableUsdcMicros ?? 0
        balance = fresh
        if previous == 0, fresh.availableUsdcMicros > 0 {
            toast = MonacoToast(message: "USDC arrived. You can fund your cabal now.", isSuccess: true)
        }
    }

    private func trackFundSweep(depositId: String, cabalName: String, amountLabel: String) {
        Task {
            await pollFundSweep(depositId: depositId, cabalName: cabalName, amountLabel: amountLabel)
        }
    }

    private func pollFundSweep(depositId: String, cabalName: String, amountLabel: String) async {
        guard let token = auth.accessToken else { return }
        var machine = DepositPollStateMachine()
        let phase = await machine.pollUntilTerminal {
            let deposit = try await apiClient.getDeposit(accessToken: token, depositId: depositId)
            return deposit.status
        }
        switch phase {
        case .credited:
            toast = MonacoToast(message: "Added \(amountLabel) to \(cabalName).", isSuccess: true)
            await onFunded()
        case .failed:
            toast = MonacoToast(message: "Couldn't add money to the cabal. Try again.", isSuccess: false)
            await onFunded()
        case .idle, .awaitingSweep:
            toast = MonacoToast(
                message: "Still adding \(amountLabel) to \(cabalName). Check activity for updates.",
                isSuccess: true
            )
        }
    }
}
