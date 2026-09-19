import MonacoCore
import SwiftUI

/// The cabal's trading bot, when it has one: name, budget, and whether it's trading.
struct AgentSectionView: View {
    let agent: GroupAgentDTO

    var body: some View {
        MonacoGroupedList {
            MonacoRow(title: agent.agentDisplayName, subtitle: subtitle, isLast: true) {
                StockMark(systemImage: "cpu")
            } trailing: {
                AgentStatusText(status: agent.status)
            }
        }
        .accessibilityIdentifier("group-agent-card")
    }

    private var subtitle: String {
        guard let micros = Int64(agent.allocationUsdcMicros) else { return "Trading bot" }
        return "Trading bot · \(UsdAmountFormatter.format(micros: micros)) budget"
    }
}

/// "Active" in ink, "Paused" in warning, "Revoked" muted. No green: green means profit.
struct AgentStatusText: View {
    let status: String

    var body: some View {
        Text(label)
            .font(MonacoTheme.Typo.caption.weight(.semibold))
            .foregroundStyle(color)
            .accessibilityIdentifier("agent-status-\(status.lowercased())")
    }

    private var label: String {
        switch status.lowercased() {
        case "active": "Active"
        case "paused": "Paused"
        case "revoked": "Revoked"
        default: status.capitalized
        }
    }

    private var color: Color {
        switch status.lowercased() {
        case "active": MonacoTheme.ink
        case "paused": MonacoTheme.warning
        default: MonacoTheme.muted
        }
    }
}
