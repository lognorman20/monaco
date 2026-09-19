import SwiftUI

struct AgentSectionView: View {
    let agent: GroupAgentDTO

    var body: some View {
        Section("Trading agent") {
            HStack {
                Text(agent.agentDisplayName)
                    .font(.headline)
                Spacer()
                AgentStatusBadge(status: agent.status)
            }
            LabeledContent("Budget") {
                Text(formatUsd(micros: agent.allocationUsdcMicros))
            }
        }
    }

    private func formatUsd(micros: String) -> String {
        guard let value = Int64(micros) else { return micros }
        return String(format: "$%.2f", Double(value) / 1_000_000.0)
    }
}

struct AgentStatusBadge: View {
    let status: String

    var body: some View {
        Text(label)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(color.opacity(0.15))
            .foregroundStyle(color)
            .clipShape(Capsule())
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
        case "active": .green
        case "paused": .orange
        case "revoked": .gray
        default: .secondary
        }
    }
}
