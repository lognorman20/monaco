import SwiftUI

struct ProposalStatusChip: View {
    let status: String
    var kind: String = "buy"

    var body: some View {
        Text(label)
            .font(.caption.bold())
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(backgroundColor.opacity(0.15))
            .foregroundStyle(backgroundColor)
            .clipShape(Capsule())
            .accessibilityIdentifier("proposal-status-\(kind.lowercased())-\(status.lowercased())")
    }

    private var chipStyle: ProposalStatusChipStyle? {
        ProposalStatusChipStyle(status: status)
    }

    private var label: String {
        switch kind.lowercased() {
        case "sell":
            return "Sell \(chipStyle?.label ?? status.capitalized)"
        case "add_agent":
            return "Add agent \(chipStyle?.label ?? status.capitalized)"
        case "pause_agent":
            return "Pause agent \(chipStyle?.label ?? status.capitalized)"
        case "resume_agent":
            return "Resume agent \(chipStyle?.label ?? status.capitalized)"
        case "revoke_agent":
            return "Revoke agent \(chipStyle?.label ?? status.capitalized)"
        default:
            return "Buy \(chipStyle?.label ?? status.capitalized)"
        }
    }

    private var backgroundColor: Color {
        switch chipStyle {
        case .open: .blue
        case .passed: .green
        case .failed: .red
        case .expired: .gray
        case .none: .secondary
        }
    }
}
