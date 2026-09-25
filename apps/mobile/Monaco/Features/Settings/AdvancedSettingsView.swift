import SwiftUI

/// Block-explorer links, reachable from Profile's account actions.
struct AdvancedSettingsView: View {
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader("Block explorers")
                    .padding(.horizontal, MonacoTheme.Space.m)
                MonacoGroupedList {
                    ForEach(SettingsAdvancedLinks.explorerLinks) { link in
                        Link(destination: link.url) {
                            MonacoRow(
                                title: link.title,
                                subtitle: link.url.host() ?? link.url.absoluteString,
                                isLast: link.id == SettingsAdvancedLinks.explorerLinks.last?.id,
                                leading: { StockMark(systemImage: "safari", size: 40) },
                                trailing: {
                                    Image(systemName: "arrow.up.right")
                                        .font(MonacoTheme.Typo.captionStrong)
                                        .foregroundStyle(MonacoTheme.muted)
                                        .accessibilityHidden(true)
                                }
                            )
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("settings-explorer-\(link.id)")
                    }
                }
                Text("Opens in Safari. Monaco never asks you to sign anything there.")
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .padding(.horizontal, MonacoTheme.Space.m)
            }
            .padding(.vertical, MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }
}

#Preview {
    NavigationStack {
        AdvancedSettingsView()
    }
}
