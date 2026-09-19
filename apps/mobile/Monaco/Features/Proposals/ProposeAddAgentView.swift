import MonacoCore
import SwiftUI

/// Propose adding a trading bot with a budget from the pot. The key is minted only after the vote passes.
struct ProposeAddAgentView: View {
    let groupId: String
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    @Environment(\.dismiss) private var dismiss
    @State private var pot: ProposePot?
    @State private var name = ""
    @State private var amountText = ""
    @State private var isSending = false
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

    var body: some View {
        ScrollView {
            VStack(spacing: MonacoTheme.Space.xl) {
                AmountEntry(
                    amountText: $amountText,
                    max: pot.map { ProposeMath.usd(fromMicros: $0.totalMicros) },
                    presets: [.dollars(50), .dollars(100), .dollars(250)],
                    helper: ProposeFlowCopy.botBudgetHelper,
                    overLimitHelper: ProposeFlowCopy.overPot
                )
                .accessibilityIdentifier("add-agent-allocation-field")

                MonacoTextField(ProposeFlowCopy.botNamePlaceholder, text: $name)
                    .accessibilityIdentifier("add-agent-name-field")

                Text(ProposeFlowCopy.botExplainer)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)

                if let errorMessage {
                    Text(errorMessage)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.loss)
                        .multilineTextAlignment(.center)
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.vertical, MonacoTheme.Space.m)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .proposeFlowFullHeight()
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
            let id = try await service.propose(groupId: groupId, draft: .addAgent(name: trimmedName, allocationMicros: budgetMicros))
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

/// Pause, resume, or remove the cabal's trading bot: one question and one button.
struct ProposeAgentLifecycleView: View {
    let groupId: String
    let kind: String
    let botName: String
    var onProposed: ((_ proposalId: String) -> Void)?

    private let service: ProposeService

    @Environment(\.dismiss) private var dismiss
    @State private var isSending = false
    @State private var errorMessage: String?

    init(service: ProposeService, groupId: String, kind: String, botName: String, onProposed: ((_ proposalId: String) -> Void)? = nil) {
        self.service = service
        self.groupId = groupId
        self.kind = kind
        self.botName = botName
        self.onProposed = onProposed
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            HStack(spacing: MonacoTheme.Space.sm) {
                StockMark(systemImage: "cpu", size: 56)
                Text(botName)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
            }
            Text(ProposeFlowCopy.lifecycleTitle(kind: kind))
                .font(MonacoTheme.Typo.display)
                .foregroundStyle(MonacoTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
            Text(ProposeFlowCopy.lifecycleMessage(kind: kind, botName: botName))
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
            if let errorMessage {
                Text(errorMessage)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.loss)
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, MonacoTheme.Space.gutter)
        .padding(.top, MonacoTheme.Space.l)
        .background(MonacoTheme.canvas.ignoresSafeArea())
        .proposeFlowFullHeight()
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
            let id = try await service.propose(groupId: groupId, draft: .agentLifecycle(kind: kind))
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
