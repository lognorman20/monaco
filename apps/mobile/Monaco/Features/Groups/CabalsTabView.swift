import MonacoCore
import SwiftUI

/// Cabals tab: P&L of your cabals, search, your cabals strip, and the
/// platform-wide board.
struct CabalsTabView: View {
    @ObservedObject var auth: DynamicAuthService
    @Environment(AppSessionStore.self) private var session
    @State private var model: CabalsTabModel
    @State private var searchText = ""
    @State private var showNewCabalSheet = false
    /// The pushed screen, if any. One item for the whole tab: see `CabalsRoute`.
    @State private var route: CabalsRoute?
    /// Chosen in the "New cabal" sheet, pushed once the sheet is gone. Pushing
    /// in the same turn as the dismissal makes the stack change mid-transition.
    @State private var routeAfterSheet: CabalsRoute?
    /// Starts true: the tab asks for the cabals list in `.task`, so on the very
    /// first body evaluation a load is about to happen. Starting at false made
    /// `stripState` compute `.unavailable` and render the hard error for a frame
    /// before anything had even been attempted.
    @State private var isLoadingCabals = true
    /// The strip card → cabal hero zoom. One namespace for the tab, so the card the member
    /// actually tapped is the thing that grows into the screen they land on.
    @Namespace private var cabalZoom

    private let actions: CabalsActionSource

    init(
        auth: DynamicAuthService,
        dataSource: CabalsTabDataSource? = nil,
        actions: CabalsActionSource? = nil
    ) {
        self.auth = auth
        self.actions = actions ?? LiveCabalsActionSource(auth: auth)
        _model = State(initialValue: CabalsTabModel(dataSource: dataSource ?? LiveCabalsTabDataSource(auth: auth)))
    }

    /// Membership as a set: the board reordering its rows is not a membership change.
    private var joinedIDs: Set<String> {
        Set(session.joinedCabals.map(\.groupId))
    }

    /// "No cabals yet" is only true once we have actually heard from the server.
    /// Until then the strip says it is loading, or offers a retry. The shell's
    /// own load counts: the state machine must never be able to claim a failure
    /// before an attempt has finished.
    private var stripState: CabalsStripState {
        if session.home != nil { return .loaded }
        return isLoadingCabals || session.isLoading ? .loading : .unavailable
    }

    var body: some View {
        MonacoScreen {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.section) {
                    MonacoSearchField(placeholder: "Find a cabal by name", text: $searchText)
                        .accessibilityIdentifier("cabals-search-field")

                    if model.isSearching {
                        CabalsSearchResultsSection(model: model, onSelect: { route = $0 })
                    } else {
                        CabalsStripSection(
                            rows: session.joinedCabals,
                            state: stripState,
                            tints: session.cabalTints,
                            zoomNamespace: cabalZoom,
                            onSelect: { route = $0 },
                            onRetry: { Task { await loadCabals() } }
                        )
                        CabalsPnLChartSection(
                            model: model,
                            hasCabals: !session.joinedCabals.isEmpty,
                            tints: session.cabalTints
                        )
                        CabalsLeaderboardSection(
                            model: model,
                            tints: session.cabalTints,
                            onSelect: { route = $0 }
                        )
                    }
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
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
        .sheet(isPresented: $showNewCabalSheet, onDismiss: {
            if let routeAfterSheet {
                route = routeAfterSheet
                self.routeAfterSheet = nil
            }
        }) {
            NewCabalSheet(
                onCreate: {
                    routeAfterSheet = .create
                    showNewCabalSheet = false
                },
                onJoin: {
                    routeAfterSheet = .joinByCode
                    showNewCabalSheet = false
                }
            )
            .presentationDetents([.medium])
        }
        .navigationDestination(item: $route) { route in
            CabalsRouteDestination(
                auth: auth,
                route: route,
                actions: actions,
                onChanged: refreshAll,
                onCreated: { created in
                    // Replace the form with the new cabal. Back then lands on the
                    // tab, not on a filled-in form that would create a second one.
                    self.route = .cabal(id: created.groupId, name: created.name)
                },
                onJoined: { groupId, groupName in
                    model.markJoined(groupID: groupId)
                    self.route = .cabal(id: groupId, name: groupName)
                },
                zoomNamespace: cabalZoom
            )
        }
        .refreshable {
            await refreshAll()
        }
        .task {
            if session.home == nil { await loadCabals() }
            await model.reload(hasCabals: !session.joinedCabals.isEmpty)
        }
        .onChange(of: searchText) { _, newValue in
            model.updateQuery(newValue)
        }
        .onChange(of: joinedIDs) { _, ids in
            // Joined, created, or left a cabal somewhere in the app.
            Task { await model.reload(hasCabals: !ids.isEmpty) }
        }
        .onChange(of: model.rejectedSession) { _, rejected in
            guard let rejected else { return }
            Task { await auth.signOutAfterRejectedSession(rejectedToken: rejected.token) }
        }
        .accessibilityIdentifier("cabals-root")
        .monacoFrameStats("Cabals")
    }

    private func refreshAll() async {
        // `session.refresh` loads the cabals list in a background task of its own,
        // so pull-to-refresh awaits that read directly: the spinner then ends when
        // the strip is actually up to date.
        async let profile: Void = session.refresh(auth: auth)
        async let cabals: Void = loadCabals()
        _ = await (profile, cabals)
        await model.reload(hasCabals: !session.joinedCabals.isEmpty)
    }

    private func loadCabals() async {
        isLoadingCabals = true
        defer { isLoadingCabals = false }
        await session.refreshHomeBoards(accessToken: auth.accessToken)
    }
}

/// The `.medium` sheet behind the tab's trailing "New cabal" button: two big
/// choices, start fresh or join with a code someone shared.
private struct NewCabalSheet: View {
    let onCreate: () -> Void
    let onJoin: () -> Void

    var body: some View {
        NavigationStack {
            VStack(spacing: MonacoTheme.Space.sm) {
                Button(action: onCreate) {
                    NewCabalChoice(
                        systemImage: "plus",
                        title: "Start a cabal",
                        subtitle: "Name it and invite friends"
                    )
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("new-cabal-create-row")

                Button(action: onJoin) {
                    NewCabalChoice(
                        systemImage: "person.badge.plus",
                        title: "Join with an invite code",
                        subtitle: "Paste a code your friend shared"
                    )
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("new-cabal-join-row")
            }
            .padding(MonacoTheme.Space.gutter)
            .frame(maxHeight: .infinity, alignment: .top)
            .monacoCanvas()
            .navigationTitle("New cabal")
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

/// One of the two ways into a cabal. A card, not a row: there are exactly two of these and the
/// sheet exists to make the choice feel like a choice.
private struct NewCabalChoice: View {
    let systemImage: String
    let title: String
    let subtitle: String

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(alignment: .top, spacing: MonacoTheme.Space.m) {
            Image(systemName: systemImage)
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(MonacoTheme.brand)
                .frame(width: 44, height: 44)
                .background(Circle().fill(MonacoTheme.brandWash))
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .displayFont(.section)
                    .foregroundStyle(MonacoTheme.fgPrimary)
                    .fixedSize(horizontal: false, vertical: true)
                Text(subtitle)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.fgMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if !dynamicTypeSize.isAccessibilitySize {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.fgSubtle)
                    .padding(.top, 14)
            }
        }
        .multilineTextAlignment(.leading)
        .padding(MonacoTheme.Space.m)
        .frame(maxWidth: .infinity, minHeight: 76, alignment: .leading)
        .monacoElevation(.card)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    let session = AppSessionStore()
    session.home = CabalsTabSampleData.home
    return NavigationStack {
        CabalsTabView(auth: DynamicAuthService(), dataSource: CabalsTabSampleData.DataSource())
            .environment(session)
            .monacoRootAppearance()
    }
}
#endif
