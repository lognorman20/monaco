import MonacoCore
import SwiftUI

/// The bot key with copy affordance: on the passed proposal (proposer only, for 15 minutes) and
/// on the agent detail screen (any cabal member, until the bot is removed).
struct AgentKeyRevealView: View {
    let apiKey: String
    var connectText: String? = nil
    var explainer: String = ProposeFlowCopy.botKeyExplainer
    /// Receives the toast message for whichever copy button was tapped.
    var onCopied: (String) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
            Text(explainer)
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
            if let connectText, !connectText.isEmpty {
                Text(ProposeFlowCopy.clawPumpSteps)
                    .font(MonacoTheme.Typo.callout)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("agent-clawpump-steps")
                Button(ProposeFlowCopy.copyConnectInstructions) {
                    copy(connectText, message: ProposeFlowCopy.connectCopied)
                }
                .buttonStyle(.monacoPrimary)
                .accessibilityIdentifier("agent-connect-copy")
            }
            Button(ProposeFlowCopy.copyKey) {
                copy(apiKey, message: ProposeFlowCopy.keyCopied)
            }
            .buttonStyle(.monacoSecondary)
            .accessibilityIdentifier("agent-key-copy")
        }
    }

    /// Device-only and expiring: what is copied here carries the bot's key.
    private func copy(_ text: String, message: String) {
        SecretPasteboard.copy(text)
        Haptics.success()
        onCopied(message)
    }
}
