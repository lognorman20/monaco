import MonacoCore
import SwiftUI

/// The bot key with copy affordance; also used on the agent detail screen.
struct AgentKeyRevealView: View {
    let apiKey: String
    var onCopied: () -> Void = {}

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            Text(ProposeFlowCopy.botKeyExplainer)
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Text(apiKey)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(MonacoTheme.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(MonacoTheme.Space.m)
                .background(MonacoTheme.surfaceSunken, in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.field, style: .continuous))
                .accessibilityIdentifier("agent-key-reveal")
            Button(ProposeFlowCopy.copyKey) {
                UIPasteboard.general.string = apiKey
                Haptics.success()
                onCopied()
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("agent-key-copy")
        }
    }
}
