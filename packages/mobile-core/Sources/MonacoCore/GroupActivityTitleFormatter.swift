import Foundation

public enum GroupActivityTitleFormatter {
    public static func format(
        kind: String,
        symbol: String?,
        agentDisplayName: String?,
        initiatedBy: String?
    ) -> String {
        let normalizedKind = kind.lowercased()
        let stock = AssetSymbolFormatter.format(symbol ?? "USDC")
        let agentName = agentDisplayName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

        switch normalizedKind {
        case "deposit":
            return "Deposit"
        case "add_agent":
            let name = agentName.isEmpty ? (symbol ?? "agent") : agentName
            return "Added \(name)"
        case "pause_agent":
            return agentName.isEmpty ? "Paused cabal agent" : "Paused \(agentName)"
        case "resume_agent":
            return agentName.isEmpty ? "Resumed cabal agent" : "Resumed \(agentName)"
        case "revoke_agent":
            return agentName.isEmpty ? "Revoked cabal agent" : "Revoked \(agentName)"
        case "buy":
            if initiatedBy?.lowercased() == "agent", !agentName.isEmpty {
                return "\(agentName) buy \(stock)"
            }
            return "Buy \(stock)"
        case "sell":
            if initiatedBy?.lowercased() == "agent", !agentName.isEmpty {
                return "\(agentName) sell \(stock)"
            }
            return "Sell \(stock)"
        default:
            return kind.capitalized
        }
    }
}
