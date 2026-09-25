import MonacoCore
import SwiftUI

/// Propose adding a trading bot with a budget from the pot. The key is minted only after the vote passes.
///
/// The budget is the hero, as the amount is on a buy, then the bot's name, then what the cabal is
/// agreeing to as ruled lines: what the bot may do, how its key reaches the member, and that the
/// cabal can stop it.
struct ProposeAddAgentView: View {
    let groupId: String
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    @Environment(\.dismiss) private var dismiss
    @State private var pot: ProposePot?
    @State private var name = ""
    @State private var amountText = ""
    @State private var isSending = false
    /// Idempotency key for the proposal being sent; a retry after a lost response reuses it.
    @State private var proposeSubmission = IdempotentSubmission()
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, pot: ProposePot?, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.onProposed = onProposed
        _pot = State(initialValue: pot)
    }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var budgetMicros: Int64? {
        ProposeMath.micros(fromAmountText: amountText)
    }

    private var isOverPot: Bool {
        guard let budgetMicros, let pot else { return false }
        return budgetMicros > pot.totalMicros
    }

    private var canSend: Bool {
        !trimmedName.isEmpty && budgetMicros != nil && !isOverPot && !isSending
    }

    private var potUsd: Decimal? {
        pot.map { ProposeMath.usd(fromMicros: $0.totalMicros) }
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                VStack(spacing: MonacoTheme.Space.m) {
                    AmountEntry(
                        amountText: $amountText,
                        max: potUsd,
                        helper: ProposeFlowCopy.botBudgetHelper,
                        overLimitHelper: ProposeFlowCopy.overPot
                    )
                    .accessibilityIdentifier("add-agent-allocation-field")

                    ProposePresetChips(
                        amountText: $amountText,
                        presets: [.dollars(50), .dollars(100), .dollars(250)],
                        max: potUsd
                    )
                }
                .padding(.top, MonacoTheme.Space.l)
                .padding(.horizontal, MonacoTheme.Space.m)

                MonacoTextField(ProposeFlowCopy.botNamePlaceholder, text: $name)
                    .accessibilityIdentifier("add-agent-name-field")
                    .padding(.top, MonacoTheme.Space.xl)
                    .padding(.horizontal, MonacoTheme.Space.m)

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader(ProposeScreenCopy.botRulesTitle)
                        .padding(.horizontal, MonacoTheme.Space.m)
                    MonacoGroupedList {
                        BotTermRow(systemImage: "arrow.up.arrow.down", text: ProposeScreenCopy.botTrades)
                        // The key handoff, in the words the passed proposal and the bot's screen use.
                        BotTermRow(systemImage: "key", text: ProposeFlowCopy.botExplainer)
                        BotTermRow(systemImage: "pause", text: ProposeScreenCopy.botAnswers, isLast: true)
                    }
                }
                .padding(.top, MonacoTheme.Space.xl)

                if let errorMessage {
                    ReceiptError(message: errorMessage)
                        .padding(.horizontal, MonacoTheme.Space.m)
                        .padding(.top, MonacoTheme.Space.l)
                }
            }
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle(ProposeFlowCopy.addBotTitle)
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button {
                    Task { await send() }
                } label: {
                    ZStack {
                        Text(ProposeFlowCopy.sendToCabal).opacity(isSending ? 0 : 1)
                        if isSending { ProgressView().tint(MonacoTheme.primaryButtonLabel) }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(!canSend)
                .accessibilityIdentifier("add-agent-submit")
            }
        }
        .task {
            if pot == nil { pot = try? await service.pot(groupId: groupId) }
        }
    }

    private func send() async {
        guard canSend, let budgetMicros else { return }
        Haptics.tap()
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        do {
            let id = try await service.propose(groupId: groupId, draft: .addAgent(name: trimmedName, allocationMicros: budgetMicros), submission: proposeSubmission)
            if let onProposed {
                onProposed(id)
            } else {
                Haptics.success()
                dismiss()
            }
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.propose(error)
            Haptics.warning()
        }
    }
}

/// One term of a bot proposal: a small glyph and one sentence, ruled like the rest of the ledger.
private struct BotTermRow: View {
    let systemImage: String
    let text: String
    var isLast = false

    var body: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            ProposeGlyph(systemImage: systemImage, size: ProposeGlyph.noteSize)
            Text(text)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, MonacoTheme.Space.sm)
        .frame(minHeight: 56)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + ProposeGlyph.noteSize + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// Pause, resume, or remove the cabal's trading bot: one question and one button.
struct ProposeAgentLifecycleView: View {
    let groupId: String
    let kind: String
    let botName: String
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    @Environment(\.dismiss) private var dismiss
    @State private var isSending = false
    /// Idempotency key for the proposal being sent; a retry after a lost response reuses it.
    @State private var proposeSubmission = IdempotentSubmission()
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, kind: String, botName: String, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.kind = kind
        self.botName = botName
        self.onProposed = onProposed
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                HStack(spacing: MonacoTheme.Space.sm) {
                    ProposeGlyph(systemImage: ProposeGlyph.bot, size: 48)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(botName)
                            .font(MonacoTheme.Typo.rowTitle)
                            .foregroundStyle(MonacoTheme.ink)
                            .lineLimit(2)
                        Text(ProposeFlowCopy.agentDetailTitle)
                            .font(MonacoTheme.Typo.caption)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                }
                .accessibilityElement(children: .combine)

                Text(ProposeFlowCopy.lifecycleTitle(kind: kind))
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                    .padding(.top, MonacoTheme.Space.s)
                Text(ProposeFlowCopy.lifecycleMessage(kind: kind, botName: botName))
                    .font(MonacoTheme.Typo.body)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                if let errorMessage {
                    ReceiptError(message: errorMessage)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.top, MonacoTheme.Space.l)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .safeAreaInset(edge: .bottom) {
            BottomCTA {
                Button {
                    Task { await send() }
                } label: {
                    ZStack {
                        Text(ProposeFlowCopy.sendToCabal).opacity(isSending ? 0 : 1)
                        if isSending { ProgressView().tint(MonacoTheme.primaryButtonLabel) }
                    }
                }
                .buttonStyle(.monacoPrimary)
                .disabled(isSending)
                .accessibilityIdentifier("agent-lifecycle-submit")
            }
        }
    }

    private func send() async {
        guard !isSending else { return }
        Haptics.tap()
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        do {
            let id = try await service.propose(groupId: groupId, draft: .agentLifecycle(kind: kind), submission: proposeSubmission)
            if let onProposed {
                onProposed(id)
            } else {
                Haptics.success()
                dismiss()
            }
        } catch {
            if error.isRequestCancellation { return }
            errorMessage = ProposeErrorCopy.propose(error)
            Haptics.warning()
        }
    }
}
