import SwiftUI

/// Block-explorer links, reachable from Profile's account actions.
///
/// It was a `Form`, which is the loudest "we did not design this" signal an iOS app has: a stock
/// grouped table with stock insets and stock separators, three screens away from a designed one.
/// It is the app's own grouped list now, and it is a smaller file for it.
struct AdvancedSettingsView: View {
    private var links: [SettingsExplorerLink] { SettingsAdvancedLinks.explorerLinks }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
                MonacoSectionHeader("Block explorers")

                MonacoGroupedList {
                    ForEach(links) { link in
                        Link(destination: link.url) {
                            MonacoRow(
                                title: link.title,
                                subtitle: SettingsAdvancedLinks.displayURL(link.url),
                                chevron: true,
                                isLast: link.id == links.last?.id
                            ) {
                                MonacoRowGlyph(systemName: "safari")
                            }
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("settings-explorer-\(link.id)")
                    }
                }
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// A quiet glyph tile in a row's leading slot, at the same 44pt as every other mark so the
/// separator inset `MonacoRowLayout` derives stays correct.
struct MonacoRowGlyph: View {
    let systemName: String
    var size: CGFloat = 44

    var body: some View {
        RoundedRectangle(cornerRadius: size * MonacoTheme.Radius.tile / 44, style: .continuous)
            .fill(MonacoTheme.fillQuiet)
            .frame(width: size, height: size)
            .overlay {
                Image(systemName: systemName)
                    .font(.system(size: size * 0.42, weight: .semibold))
                    .foregroundStyle(MonacoTheme.fgMuted)
            }
            .accessibilityHidden(true)
    }
}

#Preview {
    NavigationStack {
        AdvancedSettingsView()
    }
}
