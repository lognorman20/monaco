import MonacoCore
import SwiftUI

/// The cabal's trading bot, when it has one: name, budget, and whether it's trading.
struct AgentSectionView: View {
    let agent: GroupAgentDTO

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "cpu")
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(MonacoTheme.ink)
                .frame(width: 44, height: 44)
                .background(MonacoTheme.border.opacity(0.5), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text(agent.agentDisplayName)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(1)
                Text(subtitle)
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            AgentStatusText(status: agent.status)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .frame(minHeight: 60)
        .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous))
        .accessibilityElement(children: .combine)
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
            .font(.footnote.weight(.semibold))
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
