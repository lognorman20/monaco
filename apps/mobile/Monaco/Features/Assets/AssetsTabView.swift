import SwiftUI

/// Market browse chrome. Catalog APIs and prices are #156.
struct AssetsTabView: View {
    @State private var searchQuery = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                MonacoSearchField(
                    placeholder: "Search a stock",
                    text: $searchQuery,
                    isEnabled: true
                )
                .accessibilityIdentifier("assets-search-field")

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("Popular")
                        .font(MonacoTheme.TypeRole.title)
                        .foregroundStyle(MonacoTheme.ink)

                    MonacoCard {
                        Text("Join a cabal, then propose Apple from its pot. Names and prices will fill this strip.")
                            .font(MonacoTheme.TypeRole.body)
                            .foregroundStyle(MonacoTheme.muted)
                    }
                    .accessibilityIdentifier("assets-popular-strip")
                }

                MonacoEmptyStateCard(
                    message: "Open a cabal to propose a stock. Search and prices land here once you are in a pot.",
                    systemImage: "chart.pie"
                )
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .foregroundStyle(MonacoTheme.ink)
        .navigationTitle("Assets")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("assets-root")
    }
}
