import MonacoCore
import SwiftUI

/// The cabal's vote-gated AI trader.
///
/// This is the most novel thing in the product and it rendered as one grey row with a 13pt `cpu`
/// glyph. It is now the **only** elevated object on an otherwise flat cabal screen — the eye goes
/// there because it is the exception, not because it has been decorated. If a second thing on
/// this screen ever takes E2, neither of them is elevated any more.
///
/// What it does **not** draw, and why: there is no budget ring and no last-trade rationale.
/// `GroupAgentDTO` carries `id`, `status`, `agentDisplayName` and `allocationUsdcMicros` —
/// nothing about what has been spent and nothing about what it last did. A ring drawn at zero is
/// a claim about the cabal's money, so `spentUsdcMicros` and `lastTrade` are filed as the API
/// follow-up instead of guessed at.
struct AgentSectionView: View {
    let agent: GroupAgentDTO

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var state: AgentState { AgentState(status: agent.status) }

    private var allocation: String? {
        guard let micros = Int64(agent.allocationUsdcMicros) else { return nil }
        return UsdAmountFormatter.format(micros: micros)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            header
            if let allocation {
                Rectangle()
                    .fill(MonacoTheme.line)
                    .frame(height: 1)
                budget(allocation)
            }
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .monacoElevation(.raised)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("group-agent-card")
    }

    private var header: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.sm) {
            glyph
            VStack(alignment: .leading, spacing: 2) {
                Text("AI trader")
                    .displayFont(.eyebrow)
                    .foregroundStyle(MonacoTheme.fgSubtle)
                Text(agent.agentDisplayName)
                    .displayFont(.section)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !dynamicTypeSize.isAccessibilitySize {
                AgentStatusChip(state: state)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(agent.agentDisplayName), AI trader, \(state.label)")
    }

    private var glyph: some View {
        Image(systemName: "cpu")
            .font(.system(size: 17, weight: .semibold))
            .foregroundStyle(state.foreground)
            .frame(width: 40, height: 40)
            .background(
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.tile * 40 / 44, style: .continuous)
                    .fill(state.wash)
            )
            // A live trader gets a pulse and nothing else. Never green: green is profit.
            .symbolEffect(.pulse, isActive: state == .active && !reduceMotion)
            .accessibilityHidden(true)
    }

    private func budget(_ allocation: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Budget")
                    .displayFont(.eyebrow)
                    .foregroundStyle(MonacoTheme.fgSubtle)
                MoneyText(decimalString: allocation, style: .row)
            }
            Spacer(minLength: MonacoTheme.Space.s)
            if dynamicTypeSize.isAccessibilitySize {
                AgentStatusChip(state: state)
            } else {
                Text("It can only spend what the cabal voted it")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .multilineTextAlignment(.trailing)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("group-agent-budget")
    }
}

/// Active, paused, revoked — and never green. Green means profit.
enum AgentState: Equatable {
    case active
    case paused
    case revoked
    case other(String)

    init(status: String) {
        switch status.lowercased() {
        case "active": self = .active
        case "paused": self = .paused
        case "revoked": self = .revoked
        default: self = .other(status.capitalized)
        }
    }

    var label: String {
        switch self {
        case .active: return "Trading"
        case .paused: return "Paused"
        case .revoked: return "Revoked"
        case .other(let raw): return raw
        }
    }

    /// The raw status, for the accessibility identifier the existing tests address.
    var identifierSuffix: String {
        switch self {
        case .active: return "active"
        case .paused: return "paused"
        case .revoked: return "revoked"
        case .other(let raw): return raw.lowercased()
        }
    }

    /// Brand wash for a working agent — the interactive accent, because a live agent is the one
    /// thing on this card you might want to go and look at. Amber for paused, quiet for revoked.
    var wash: Color {
        switch self {
        case .active: return MonacoTheme.brandWash
        case .paused: return MonacoTheme.warningWash
        case .revoked, .other: return MonacoTheme.fillQuiet
        }
    }

    var foreground: Color {
        switch self {
        case .active: return MonacoTheme.brandOnWash
        case .paused: return MonacoTheme.warningOnWash
        case .revoked, .other: return MonacoTheme.fgMuted
        }
    }
}

/// The state as a capsule beside the agent's name.
struct AgentStatusChip: View {
    let state: AgentState

    var body: some View {
        Text(state.label)
            .font(MonacoTheme.Typo.micro)
            .foregroundStyle(state.foreground)
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
            .background(Capsule().fill(state.wash))
            .accessibilityIdentifier("agent-status-\(state.identifierSuffix)")
    }
}
