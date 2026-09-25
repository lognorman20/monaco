import MonacoCore
import SwiftUI

/// The cabal's trading bot, when it has one: a section on the cabal screen with one ruled
/// row — the bot's mark, its name over its budget, and whether it is trading.
struct AgentSectionView: View {
    let agent: GroupAgentDTO
    /// Kept for the cabal screen's call site, and deliberately not called. The key is copied on
    /// the bot's own screen, which is pushed over the cabal screen, so that screen confirms the
    /// copy where the member is looking. A toast raised from here lands on the covered cabal
    /// screen and is never seen.
    var onCopied: (String) -> Void = { _ in }

    private var status: TradingBotStatus { TradingBotStatus(agent.status) }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(TradingBotCopy.sectionTitle)
                .padding(.horizontal, MonacoTheme.Space.m)

            MonacoGroupedList {
                NavigationLink {
                    AgentDetailView(agent: agent)
                } label: {
                    MonacoRow(
                        title: agent.agentDisplayName,
                        // A removed bot has no budget to speak of; its status says what happened.
                        subtitle: status.isRemoved ? nil : TradingBotCopy.budgetLine(allocationUsdcMicros: agent.allocationUsdcMicros),
                        chevron: true,
                        isLast: true
                    ) {
                        SunkenGlyphMark(systemImage: "cpu", isMuted: status.isRemoved)
                    } trailing: {
                        AgentStatusText(status: agent.status)
                    }
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("group-agent-card")
            }
        }
    }
}

/// "Active" in ink, "Paused" in amber, "Removed" muted. No green: green means profit.
struct AgentStatusText: View {
    let status: String

    var body: some View {
        let state = TradingBotStatus(status)
        Text(state.label)
            .font(MonacoTheme.Typo.captionStrong)
            .foregroundStyle(state.color)
            .accessibilityIdentifier("agent-status-\(status.lowercased())")
    }
}

/// What the server's bot status means to a member. The raw value stays the server's; the
/// label is the app's.
enum TradingBotStatus: Equatable {
    case active
    case paused
    /// The server calls it "revoked". The member voted to *remove* the bot — that is the
    /// proposal's own wording — so that is the word the status uses.
    case removed
    /// A status this build does not know yet, shown as the server sent it.
    case other(String)

    init(_ raw: String) {
        switch raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased() {
        case "active": self = .active
        case "paused": self = .paused
        case "revoked": self = .removed
        default: self = .other(raw)
        }
    }

    var label: String {
        switch self {
        case .active: "Active"
        case .paused: "Paused"
        case .removed: "Removed"
        case .other(let raw): raw.capitalized
        }
    }

    var isRemoved: Bool { self == .removed }

    /// Ink when it is trading, amber while a vote has it paused, quiet once it cannot trade.
    var color: Color {
        switch self {
        case .active: MonacoTheme.ink
        case .paused: MonacoTheme.warning
        case .removed, .other: MonacoTheme.muted
        }
    }
}

/// The trading bot in the member's words.
enum TradingBotCopy {
    static let sectionTitle = "Trading bot"
    static let budgetTitle = "Budget"
    static let budgetDetail = "From the pot"
    static let removedKey = "This bot was removed. It can't trade, and its key no longer works."

    /// "$100.00 budget" under the bot's name. Nil when the server's figure does not parse,
    /// rather than a made-up zero.
    static func budgetLine(allocationUsdcMicros: String) -> String? {
        guard let micros = Int64(allocationUsdcMicros.trimmingCharacters(in: .whitespacesAndNewlines)) else { return nil }
        return "\(UsdAmountFormatter.format(micros: micros)) budget"
    }

    static let auditedStrings: [String] = [
        sectionTitle, budgetTitle, budgetDetail, removedKey,
        TradingBotStatus.active.label, TradingBotStatus.paused.label, TradingBotStatus.removed.label,
    ]
}
