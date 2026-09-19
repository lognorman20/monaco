import MonacoCore
import SwiftUI

/// Cabals tab: P&L of your cabals, search, your cabals strip, and the
/// platform-wide board.
struct CabalsTabView: View {
    /// Where the trailing "New cabal" sheet sends you next.
    private enum DiscoveryRoute: Identifiable {
        case create
        case join

        var id: Self { self }
    }

    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session
    @State private var model: CabalsTabModel
    @State private var searchText = ""
    @State private var showNewCabalSheet = false
    @State private var discoveryRoute: DiscoveryRoute?

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
                        CabalsStripSection(auth: auth, rows: session.joinedCabals, onChanged: refreshAll)
                        CabalsPnLChartSection(model: model, hasCabals: !session.joinedCabals.isEmpty)
                        CabalsLeaderboardSection(auth: auth, model: model, onChanged: refreshAll)
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    showNewCabalSheet = true
                } label: {
                    Image(systemName: "plus")
                        .monacoToolbarIcon()
                        .frame(width: 44, height: 44)
                }
                .accessibilityLabel("New cabal")
                .accessibilityIdentifier("cabals-new-button")
            }
        }
        .sheet(isPresented: $showNewCabalSheet) {
            NewCabalSheet(
                onCreate: {
                    showNewCabalSheet = false
                    discoveryRoute = .create
                },
                onJoin: {
                    showNewCabalSheet = false
                    discoveryRoute = .join
                }
            )
            .presentationDetents([.medium])
        }
        .navigationDestination(item: $discoveryRoute) { route in
            switch route {
            case .create:
                CreateGroupView(auth: auth)
            case .join:
                JoinGroupView(auth: auth)
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

/// The `.medium` sheet behind the tab's trailing "New cabal" button: two big
/// choices, start fresh or join with a code someone shared.
private struct NewCabalSheet: View {
    let onCreate: () -> Void
    let onJoin: () -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: MonacoTheme.Space.s) {
                Button(action: onCreate) {
                    MonacoRowCard(
                        systemImage: "plus",
                        title: "Start a cabal",
                        subtitle: "Name it and invite friends",
                        trailing: nil
                    )
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("new-cabal-create-row")

                Button(action: onJoin) {
                    MonacoRowCard(
                        systemImage: "person.badge.plus",
                        title: "Join with an invite code",
                        subtitle: "Paste a code your friend shared",
                        trailing: nil
                    )
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("new-cabal-join-row")
            }
            .padding(MonacoTheme.Space.m)
            .padding(.top, MonacoTheme.Space.m)
            .frame(maxHeight: .infinity, alignment: .top)
            .monacoCanvas()
            .navigationTitle("New cabal")
            .navigationBarTitleDisplayMode(.inline)
        }
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
