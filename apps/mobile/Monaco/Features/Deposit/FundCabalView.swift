import MonacoCore
import SwiftUI
import UIKit

/// Fund this cabal: move account balance into a joined cabal's pot.
///
/// Owns the balance, the submission and its idempotency key, the sweep it watches and the
/// toasts. `FundCabalContent` is the layout.
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

    var body: some View {
        FundCabalContent(
            phase: balanceLoader.phase,
            joinedCabals: joinedCabals,
            isSingleCabalContext: isSingleCabalContext,
            selectedGroupId: $selectedGroupId,
            amountText: $amountText,
            isSubmitting: isSubmitting,
            onSubmit: { Task { await submitFund() } },
            onRetry: { Task { await balanceLoader.load(accessToken: auth.accessToken) } },
            onCopyAddress: copyAddress
        )
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

/// Which of its states Fund this cabal is in. The amount pad and its button belong together:
/// both show in `.amount` and nowhere else, so a reload never takes one away without the other.
enum FundCabalStage: Equatable {
    /// Nothing to show yet: the first balance read is still running.
    case loading
    /// The first read failed. Carries what to tell the member.
    case failed(String)
    case noCabals
    /// A balance with nothing in it: the member has to add money before they can fund.
    case needsMoney(PlatformBalanceDTO)
    case amount(PlatformBalanceDTO)

    static func resolve(phase: PlatformBalanceLoader.Phase, hasCabals: Bool) -> FundCabalStage {
        switch phase {
        case .loading:
            return .loading
        case .failed(let message):
            return .failed(message)
        case .loaded(let balance):
            if !hasCabals { return .noCabals }
            if balance.availableUsdcMicros <= 0 { return .needsMoney(balance) }
            return .amount(balance)
        }
    }

    var showsAmountEntry: Bool {
        if case .amount = self { return true }
        return false
    }
}

/// What Fund this cabal says and allows for the amount typed against the balance it has.
struct FundCabalForm: Equatable {
    let amountText: String
    let availableMicros: Int64?
    let pendingAllocationMicros: Int64

    init(amountText: String, balance: PlatformBalanceDTO?) {
        self.amountText = amountText
        availableMicros = balance?.availableUsdcMicros
        pendingAllocationMicros = balance?.pendingAllocationMicros ?? 0
    }

    /// The most the pad takes: the whole balance. Nil while there is nothing to fund with.
    var maxDollars: Decimal? {
        guard let availableMicros, availableMicros > 0 else { return nil }
        return Decimal(availableMicros) / Decimal(1_000_000)
    }

    /// The button reads the amount, so the member sees what they are about to send.
    var ctaTitle: String {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return "Add money" }
        return "Add \(AmountEntryText.display(amountText)) to the pot"
    }

    var canSubmit: Bool {
        guard let value = AmountEntryText.decimal(amountText), value > 0 else { return false }
        if let maxDollars { return value <= maxDollars }
        return false
    }

    /// The line under the figure: what there is to fund with, and what is already on its way.
    var availability: String? {
        guard let availableMicros else { return nil }
        let available = "\(UsdAmountFormatter.format(micros: availableMicros)) available"
        guard let pending = PlatformBalanceCard.pendingLine(micros: pendingAllocationMicros) else { return available }
        return "\(available) · \(pending)"
    }

    /// What funding does, under the pad. Cash out's says the same thing the other way round.
    /// With a cabal to pick, it names the one picked: the picker sits below the pad, and this is
    /// the line still on screen while the member types.
    static func note(into cabalName: String?) -> String {
        guard let cabalName, !cabalName.isEmpty else {
            return "The money leaves your account balance and joins the pot. Your slice grows by the same amount."
        }
        return "The money leaves your account balance and joins the \(cabalName) pot. Your slice grows by the same amount."
    }
}

/// Fund this cabal's layout: the amount as the hero, the balance it comes out of, the cabals to
/// choose from when there is more than one, and a button that reads the amount.
///
/// Pure: what the screen knows comes in, what the member does goes out.
struct FundCabalContent: View {
    let phase: PlatformBalanceLoader.Phase
    let joinedCabals: [HomeGroupBoardRowDTO]
    /// Opened from one cabal's screen: no picker, and the title says "this cabal".
    let isSingleCabalContext: Bool
    @Binding var selectedGroupId: String?
    @Binding var amountText: String
    let isSubmitting: Bool
    let onSubmit: () -> Void
    let onRetry: () -> Void
    let onCopyAddress: (String) -> Void

    private var stage: FundCabalStage {
        .resolve(phase: phase, hasCabals: !joinedCabals.isEmpty)
    }

    private var form: FundCabalForm {
        if case .loaded(let balance) = phase {
            return FundCabalForm(amountText: amountText, balance: balance)
        }
        return FundCabalForm(amountText: amountText, balance: nil)
    }

    private var showsPicker: Bool {
        !isSingleCabalContext && !joinedCabals.isEmpty
    }

    private var selectedCabalName: String? {
        joinedCabals.first(where: { $0.groupId == selectedGroupId })?.name
    }

    var body: some View {
        ScrollView {
            // No horizontal padding on the stack: the picker's rules run edge to edge, and the
            // amount insets itself. The amount comes first even when there is a cabal to pick:
            // the pad rises on arrival, and a picker above it pushed the chips and the helper
            // under the keyboard. The note under the pad names the cabal instead, so the
            // destination is on screen while the member types.
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                stageContent
                if showsPicker, stage.showsAmountEntry {
                    cabalPicker
                }
            }
            // The figure starts where Cash out's does, so the two read as one pair.
            .padding(.top, MonacoTheme.Space.xl)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .safeAreaInset(edge: .bottom) {
            if stage.showsAmountEntry {
                BottomCTA {
                    Button(isSubmitting ? "Adding money…" : form.ctaTitle, action: onSubmit)
                        .buttonStyle(.monacoPrimary)
                        .disabled(isSubmitting || selectedGroupId == nil || !form.canSubmit)
                        .accessibilityIdentifier("fund-cabal-submit-button")
                }
            }
        }
        .navigationTitle(isSingleCabalContext ? "Fund this cabal" : "Fund a cabal")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("fund-cabal-view")
    }

    @ViewBuilder
    private var stageContent: some View {
        switch stage {
        case .loading:
            AmountEntrySkeleton()
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .accessibilityIdentifier("fund-cabal-loading")
        case .failed(let message):
            EmptyState(
                title: "Balance unavailable",
                message: message,
                actionTitle: "Try again",
                action: onRetry
            )
            .accessibilityIdentifier("fund-cabal-balance-error")
        case .noCabals:
            EmptyState(
                title: "No cabal to fund",
                message: "Join a cabal first, then fund it from your account balance."
            )
        case .needsMoney(let balance):
            needsMoney(balance)
        case .amount:
            amountEntry
        }
    }

    // MARK: - Picker

    /// Each joined cabal as a row, the chosen one ticked. The pot is on the row so the member
    /// can tell two cabals with similar names apart by what is in them.
    private var cabalPicker: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Pick a cabal")
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                ForEach(joinedCabals) { cabal in
                    let isSelected = cabal.groupId == selectedGroupId
                    Button {
                        guard !isSelected else { return }
                        Haptics.selection()
                        selectedGroupId = cabal.groupId
                    } label: {
                        MonacoRow(
                            title: cabal.name,
                            subtitle: CabalPositionRowFigures.potSubtitle(potValueUsd: cabal.potValueUsd),
                            isLast: cabal.groupId == joinedCabals.last?.groupId
                        ) {
                            CabalMark(groupId: cabal.groupId, name: cabal.name, pictureUrl: cabal.pictureUrl)
                        } trailing: {
                            Image(systemName: "checkmark")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(MonacoTheme.brand)
                                .opacity(isSelected ? 1 : 0)
                                .accessibilityHidden(true)
                        }
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityAddTraits(isSelected ? [.isSelected] : [])
                    .accessibilityIdentifier("fund-cabal-picker-\(cabal.groupId)")
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("fund-cabal-picker")
    }

    // MARK: - Amount

    private var amountEntry: some View {
        VStack(spacing: MonacoTheme.Space.l) {
            AmountEntry(
                amountText: $amountText,
                max: form.maxDollars,
                presets: [.dollars(25), .dollars(50), .dollars(100), .fraction(1, label: "Max")],
                helper: form.availability,
                showsKeyboardDoneButton: true
            )
            AmountEntryNote(FundCabalForm.note(into: showsPicker ? selectedCabalName : nil))
        }
        .padding(.horizontal, MonacoTheme.Space.gutter)
    }

    // MARK: - Nothing to fund with

    /// The balance at zero, and the address that fills it — the same card Add money leads with.
    private func needsMoney(_ balance: PlatformBalanceDTO) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
            MonacoGroupedList {
                PlatformBalanceCard(balance: balance)
            }

            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader("Add money first")
                Text("Send USDC on Solana to your deposit address. Your account balance updates when it arrives, then you can fund \(isSingleCabalContext ? "this cabal" : "a cabal").")
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, MonacoTheme.Space.m)

            DepositAddressCard(
                content: DepositAddress.usable(balance.memberWalletAddress).map(DepositAddressCard.Content.ready)
                    ?? .unavailable("Deposit address not ready yet."),
                addressIdentifier: "fund-cabal-deposit-address",
                copyIdentifier: "fund-cabal-copy-deposit-address",
                onCopy: onCopyAddress
            )
            .padding(.horizontal, MonacoTheme.Space.m)
        }
    }
}
