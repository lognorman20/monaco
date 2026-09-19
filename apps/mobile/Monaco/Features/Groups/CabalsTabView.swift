import SwiftUI

/// Dedicated cabals list. Discovery search/charts wait for #148.
struct CabalsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    var body: some View {
        CabalListSection(
            auth: auth,
            rows: session.home?.groups ?? [],
            onRefresh: { await session.refresh(auth: auth) },
            emptyMessage: "No cabals yet. Create or join one to start investing together.",
            rowPrefix: "cabals-row"
        )
        .monacoCanvas()
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    NavigationLink {
                        CreateGroupView(auth: auth)
                    } label: {
                        Label("Create cabal", systemImage: "plus")
                    }
                    NavigationLink {
                        JoinGroupView(auth: auth)
                    } label: {
                        Label("Join cabal", systemImage: "person.badge.plus")
                    }
                } label: {
                    Image(systemName: "plus.circle")
                        .monacoToolbarIcon()
                }
                .accessibilityIdentifier("cabals-create-join-menu")
            }
        }
        .refreshable {
            await session.refresh(auth: auth)
        }
    }
}
