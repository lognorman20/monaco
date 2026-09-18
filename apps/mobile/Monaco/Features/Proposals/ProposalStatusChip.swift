import MonacoCore
import SwiftUI

struct ProposalStatusChip: View {
    let status: String
    var kind: String = "buy"

    var body: some View {
        Text(label)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(tint.opacity(0.15))
            .foregroundStyle(tint)
            .clipShape(Capsule())
            .accessibilityIdentifier("proposal-status-\(kind.lowercased())-\(status.lowercased())")
    }

    private var display: ProposalStatusDisplay? {
        ProposalStatusDisplay.from(status: status)
    }

    private var label: String {
        let state = display?.label ?? status.capitalized
        switch kind.lowercased() {
        case "sell": return "Sell \(state)"
        case "add_agent": return "Add agent \(state)"
        case "pause_agent": return "Pause agent \(state)"
        case "resume_agent": return "Resume agent \(state)"
        case "revoke_agent": return "Revoke agent \(state)"
        default: return "Buy \(state)"
        }
    }

    private var tint: Color {
        switch display {
        case .open: MonacoTheme.accent
        case .passed: MonacoTheme.success
        case .failed: MonacoTheme.destructive
        case .expired, .none: MonacoTheme.secondaryText
        }
    }
}
