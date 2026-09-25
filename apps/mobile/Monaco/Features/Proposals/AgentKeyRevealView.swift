import MonacoCore
import SwiftUI
import UIKit

/// The bot's key as a card, because it is the thing a member acts on: the key in the market's
/// mono, a line on where it goes, and the one button that copies it. Shown on the passed
/// proposal (proposer only, for 15 minutes) and on the bot's screen (any cabal member, until
/// the bot is removed).
///
/// The key wraps by character and is never hyphenated: a hyphen inside it would read as part
/// of the key. It is not selectable either. Copy goes through `SecretPasteboard`, which keeps
/// the key on this device and clears it after a couple of minutes; a text selection would put
/// it on the ordinary pasteboard and around Universal Clipboard.
struct AgentKeyRevealView: View {
    let apiKey: String
    /// The ClawPump connect text the backend writes for this bot, when it has one.
    var connectText: String? = nil
    var explainer: String = ProposeFlowCopy.botKeyExplainer
    /// Receives the toast message for whichever copy button was tapped.
    var onCopied: (String) -> Void = { _ in }

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
            MonacoWalletAddressText(address: apiKey, textStyle: .subheadline, allowsSelection: false)
                .accessibilityIdentifier("agent-key-reveal")

            Text(explainer)
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.muted)
                .fixedSize(horizontal: false, vertical: true)

            if let connectText, !connectText.isEmpty {
                Text(ProposeFlowCopy.clawPumpSteps)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("agent-clawpump-steps")
                Button(ProposeFlowCopy.copyConnectInstructions) {
                    copy(connectText, message: ProposeFlowCopy.connectCopied)
                }
                .buttonStyle(.monacoPrimary)
                .monacoFullWidthButtons()
                .accessibilityIdentifier("agent-connect-copy")
            }

            // With connect instructions on the card, copying them is the primary act and the
            // bare key steps back; without them the key is the only thing to copy.
            if connectText?.isEmpty == false {
                Button(ProposeFlowCopy.copyKey) {
                    copy(apiKey, message: ProposeFlowCopy.keyCopied)
                }
                .buttonStyle(.monacoSecondary)
                .monacoFullWidthButtons()
                .accessibilityIdentifier("agent-key-copy")
            } else {
                Button(ProposeFlowCopy.copyKey) {
                    copy(apiKey, message: ProposeFlowCopy.keyCopied)
                }
                .buttonStyle(.monacoPrimary)
                .monacoFullWidthButtons()
                .accessibilityIdentifier("agent-key-copy")
            }
        }
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
    }

    /// Device-only and expiring: both texts carry the bot's key.
    private func copy(_ text: String, message: String) {
        SecretPasteboard.copy(text)
        Haptics.success()
        onCopied(message)
    }
}
