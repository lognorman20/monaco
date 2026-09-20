import MonacoCore
import SwiftUI

/// Full agent screen: name, budget, status, and the bot API key cabal members can copy until the bot is removed.
struct AgentDetailView: View {
    let agent: GroupAgentDTO
    var onCopied: () -> Void = {}

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                MonacoGroupedList {
                    MonacoRow(title: agent.agentDisplayName, subtitle: budgetSubtitle, isLast: true) {
                        StockMark(systemImage: "cpu")
                    } trailing: {
                        AgentStatusText(status: agent.status)
                    }
                }
                .accessibilityIdentifier("agent-detail-summary")

                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    MonacoSectionHeader(ProposeFlowCopy.agentKeySection)
                    if let key = agent.apiKey, !key.isEmpty {
                        AgentKeyRevealView(apiKey: key, explainer: ProposeFlowCopy.agentKeyExplainer, onCopied: onCopied)
                    } else {
                        Text(ProposeFlowCopy.agentKeyMissing)
                            .font(MonacoTheme.Typo.callout)
                            .foregroundStyle(MonacoTheme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                            .accessibilityIdentifier("agent-key-missing")
                    }
                }
                .accessibilityIdentifier("agent-detail-key-section")
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollIndicators(.hidden)
        .monacoCanvas()
        .navigationTitle(ProposeFlowCopy.agentDetailTitle)
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("agent-detail-screen")
    }

    private var budgetSubtitle: String {
        guard let micros = Int64(agent.allocationUsdcMicros) else { return "Trading bot" }
        return "\(UsdAmountFormatter.format(micros: micros)) budget"
    }
}
