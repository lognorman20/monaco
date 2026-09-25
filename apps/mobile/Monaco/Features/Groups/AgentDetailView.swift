import MonacoCore
import SwiftUI

/// Which key section the bot's screen shows. Decided once from the status and the key, so a
/// removed bot can never offer a key that no longer works.
enum TradingBotKeyState: Equatable {
    /// The key a cabal member copies into the bot.
    case key(String)
    /// The bot was removed by a vote: there is no key, and that is why.
    case removed
    /// Trading or paused, but the server has no key on file for it.
    case missing

    static func resolve(status: String, apiKey: String?) -> TradingBotKeyState {
        if TradingBotStatus(status).isRemoved { return .removed }
        let key = apiKey?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return key.isEmpty ? .missing : .key(key)
    }
}

/// The trading bot's own screen: who it is and whether it is trading, its budget from the pot,
/// and the key a cabal member copies into the bot until the bot is removed.
struct AgentDetailView: View {
    let agent: GroupAgentDTO

    /// This screen confirms the copy itself. It is pushed over the cabal screen, so a toast
    /// raised there would be covered by this one.
    @State private var toast: MonacoToast?

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var status: TradingBotStatus { TradingBotStatus(agent.status) }

    private var keyState: TradingBotKeyState {
        .resolve(status: agent.status, apiKey: agent.apiKey)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                    header
                        .padding(.horizontal, MonacoTheme.Space.m)
                    // A removed bot's budget is history, not something it can still spend.
                    if !status.isRemoved {
                        budget
                    }
                }
                keySection
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollIndicators(.hidden)
        .monacoCanvas()
        .navigationTitle(ProposeFlowCopy.agentDetailTitle)
        .navigationBarTitleDisplayMode(.inline)
        .monacoToast($toast)
        .accessibilityIdentifier("agent-detail-screen")
    }

    // MARK: - Who

    /// The bot's mark, its name, and its status, set like the head of a ledger page.
    private var header: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            SunkenGlyphMark(systemImage: "cpu", size: 56, isMuted: status.isRemoved)
            VStack(alignment: .leading, spacing: 2) {
                Text(agent.agentDisplayName)
                    .font(MonacoTheme.Typo.title)
                    .foregroundStyle(status.isRemoved ? MonacoTheme.muted : MonacoTheme.ink)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
                AgentStatusText(status: agent.status)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .accessibilityIdentifier("agent-detail-summary")
    }

    // MARK: - Budget

    /// One ruled row: what the bot may spend, in the brand's voice — it is the cabal's money.
    private var budget: some View {
        MonacoGroupedList {
            let layout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.xs))
                : AnyLayout(HStackLayout(alignment: .center, spacing: MonacoTheme.Space.sm))
            layout {
                VStack(alignment: .leading, spacing: 2) {
                    Text(TradingBotCopy.budgetTitle)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.ink)
                    Text(TradingBotCopy.budgetDetail)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.muted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                budgetFigure
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, MonacoTheme.Space.s)
            .frame(minHeight: 60)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("agent-detail-budget")
        }
    }

    @ViewBuilder
    private var budgetFigure: some View {
        if let micros = Int64(agent.allocationUsdcMicros.trimmingCharacters(in: .whitespacesAndNewlines)) {
            MoneyText(micros: micros, style: .row)
        } else {
            Text("—")
                .moneyFont(.row)
                .foregroundStyle(MonacoTheme.muted)
        }
    }

    // MARK: - The key

    private var keySection: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(ProposeFlowCopy.agentKeySection)
            switch keyState {
            case .key(let key):
                AgentKeyRevealView(apiKey: key, connectText: agent.connectText, explainer: ProposeFlowCopy.agentKeyExplainer) { message in
                    toast = MonacoToast(message: message, isSuccess: true)
                }
            case .removed:
                note(TradingBotCopy.removedKey)
                    .accessibilityIdentifier("agent-key-removed")
            case .missing:
                note(ProposeFlowCopy.agentKeyMissing)
                    .accessibilityIdentifier("agent-key-missing")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("agent-detail-key-section")
    }

    private func note(_ text: String) -> some View {
        Text(text)
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.muted)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}
