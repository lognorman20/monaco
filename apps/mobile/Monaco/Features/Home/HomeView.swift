import SwiftUI

/// Home tab placeholder — lane 157 owns the home dashboard.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    var onRefresh: () async -> Void = {}

    var body: some View {
        List {
            MonacoEmptyStateCard(
                message: "Your home dashboard is coming soon. Open Groups for cabals or Assets for holdings.",
                systemImage: "house"
            )
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Home")
        .refreshable {
            await onRefresh()
        }
    }
}

#Preview {
    NavigationStack {
        HomeView(auth: PrivyAuthService())
            .monacoRootAppearance()
    }
}
