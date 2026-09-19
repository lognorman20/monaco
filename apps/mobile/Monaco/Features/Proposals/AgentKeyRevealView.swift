import SwiftUI

struct AgentKeyRevealView: View {
    let apiKey: String
    var onCopied: () -> Void = {}

    var body: some View {
        Section("API key") {
            Text("Copy this key now. It is shown once and not stored in the app.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Text(apiKey)
                .font(.footnote.monospaced())
                .textSelection(.enabled)
                .accessibilityIdentifier("agent-key-reveal")
            Button("Copy key") {
                UIPasteboard.general.string = apiKey
                onCopied()
            }
            .accessibilityIdentifier("agent-key-copy")
            Text("Operator guide: docs/agent-trading.md in the Monaco repo (HTTP + curl for listing assets and posting intents).")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}
