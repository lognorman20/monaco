import SwiftUI

/// Block-explorer links, reachable from Profile's account actions.
struct AdvancedSettingsView: View {
    var body: some View {
        Form {
            Section("Block explorers") {
                ForEach(SettingsAdvancedLinks.explorerLinks) { link in
                    Link(destination: link.url) {
                        Label(link.title, systemImage: "safari")
                            .foregroundStyle(MonacoTheme.accent)
                    }
                    .accessibilityIdentifier("settings-explorer-\(link.id)")
                }
            }
        }
        .monacoFormScreen()
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview {
    NavigationStack {
        AdvancedSettingsView()
    }
}
