import MonacoCore
import SwiftUI

/// Cabals tab: P&L of your cabals, search, your cabals strip, and the
/// platform-wide board.
struct CabalsTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session
    @State private var model: CabalsTabModel
    @State private var searchText = ""

    init(auth: PrivyAuthService, dataSource: CabalsTabDataSource? = nil) {
        self.auth = auth
        _model = State(initialValue: CabalsTabModel(dataSource: dataSource ?? LiveCabalsTabDataSource(auth: auth)))
    }

    private var joinedIDs: [String] {
        session.joinedCabals.map(\.groupId)
    }

    var body: some View {
        MonacoScreen {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                    MonacoSearchField(placeholder: "Find a cabal by name", text: $searchText)
                        .accessibilityIdentifier("cabals-search-field")

                    if model.isSearching {
                        CabalsSearchResultsSection(auth: auth, model: model, onChanged: refreshAll)
                    } else {
                        CabalsPnLChartSection(model: model, hasCabals: !session.joinedCabals.isEmpty)
                        CabalsStripSection(auth: auth, rows: session.joinedCabals, onChanged: refreshAll)
                        CabalsLeaderboardSection(auth: auth, model: model, onChanged: refreshAll)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .scrollDismissesKeyboard(.interactively)
        }
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
            await refreshAll()
        }
        .task {
            await model.reload()
        }
        .onChange(of: searchText) { _, newValue in
            model.updateQuery(newValue)
        }
        .onChange(of: joinedIDs) { _, _ in
            // Joined, created, or left a cabal somewhere in the app.
            Task { await model.reload() }
        }
        .onChange(of: model.sessionExpired) { _, expired in
            if expired {
                Task { await auth.logout() }
            }
        }
        .accessibilityIdentifier("cabals-root")
    }

    private func refreshAll() async {
        await session.refresh(auth: auth)
        await model.reload()
    }
}

#if DEBUG
#Preview {
    let session = AppSessionStore()
    session.home = CabalsTabSampleData.home
    return NavigationStack {
        CabalsTabView(auth: PrivyAuthService(), dataSource: CabalsTabSampleData.DataSource())
            .environment(session)
            .monacoRootAppearance()
    }
}
#endif
