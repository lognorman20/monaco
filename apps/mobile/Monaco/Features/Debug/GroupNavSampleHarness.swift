#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the *real* `GroupDetailView` reached the way the app really reaches it, and the
/// screens around it, on canned data and with no backend.
///
/// `GroupDetailSampleHarness` only ever shows `GroupDetailContent` at the root of a
/// `NavigationStack`, so it cannot catch a bug in how `GroupDetailView` behaves once the
/// cabal screen is itself a pushed screen. The first three entries push the real
/// `GroupDetailView` through each entry chain the product uses:
///
/// - `root`   — cabal screen at the stack root (baseline).
/// - `list`   — tab root → `NavigationLink` row → cabal screen (Home/Cabals lists).
/// - `create` — tab root → pushed "New cabal" form → cabal screen (`CreateGroupView`).
///
/// The rest open one real screen each, pushed over a Cabals root the way the tab pushes it:
///
/// - `start`      — Start a cabal; Create cabal lands on the new cabal.
/// - `newCabal`   — the New cabal sheet over the tab.
/// - `joinCode`   — join by pasting an invite code.
/// - `join`       — join an open cabal picked from the board.
/// - `askToJoin`  — ask to join a cabal whose admin approves members.
/// - `bots`       — the trading bot's row on the cabal screen: active, paused, removed.
/// - `bot`        — the bot's screen with the key a member copies.
/// - `botRemoved` — the bot's screen after a vote removed it.
///
/// Launch with `-MonacoGroupNavSample <entry>`.
enum GroupNavSampleEntry: String, CaseIterable {
    case root
    case list
    case create
    case start
    case newCabal
    case joinCode
    case join
    case askToJoin
    case bots
    case bot
    case botRemoved

    static let launchArgument = "-MonacoGroupNavSample"

    static var requested: GroupNavSampleEntry? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return GroupNavSampleEntry(rawValue: arguments[flag + 1])
    }
}

struct GroupNavSampleHarness: View {
    let entry: GroupNavSampleEntry
    @ObservedObject var auth: PrivyAuthService

    @State private var session: AppSessionStore = {
        let session = AppSessionStore()
        session.isLoading = false
        session.me = MeResponse(
            userId: GroupDetailSampleData.viewerId,
            displayName: "Logan Norman",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: nil,
            createdAt: nil
        )
        return session
    }()

    /// Mirrors `CabalsTabView.discoveryRoute`: the tab root pushes the create form.
    @State private var discoveryRoute: DiscoveryStub?

    /// The screens pushed over the Cabals root by every entry after `create`.
    @State private var path: [GroupNavSampleScreen]
    @State private var showNewCabal = false
    @State private var didOpenNewCabal = false
    /// Chosen in the New cabal sheet and pushed once the sheet is gone, as `CabalsTabView` does:
    /// pushing in the same turn as the dismissal changes the stack mid-transition.
    @State private var screenAfterSheet: GroupNavSampleScreen?

    private enum DiscoveryStub: Identifiable, Hashable {
        case create
        var id: Self { self }
    }

    init(entry: GroupNavSampleEntry, auth: PrivyAuthService) {
        self.entry = entry
        self.auth = auth
        _path = State(initialValue: entry.initialPath)
    }

    private var sample: GroupViewDTO { GroupDetailSampleData.view }

    private let actions = CabalsTabSampleData.Actions()

    var body: some View {
        // The live app runs every stack inside a `TabView`; keep that here so the harness
        // exercises the same navigation container the bug was reported against.
        TabView {
            tab
                .tabItem {
                    Label("Cabals", systemImage: "person.3")
                        .accessibilityIdentifier("tab-cabals")
                }
        }
        .tint(MonacoTheme.ink)
        .environment(session)
    }

    @ViewBuilder
    private var tab: some View {
        if entry.isNavigationChain {
            NavigationStack {
                stack
            }
        } else {
            NavigationStack(path: $path) {
                screensRoot
                    .navigationDestination(for: GroupNavSampleScreen.self) { screen in
                        destination(screen)
                    }
            }
        }
    }

    @ViewBuilder
    private var stack: some View {
        switch entry {
        case .list:
            listRoot
        case .create:
            createRoot
        default:
            cabalScreen
        }
    }

    /// The real cabal screen, fed from sample data so it never calls the backend.
    private var cabalScreen: some View {
        GroupDetailView(
            auth: auth,
            groupId: sample.id,
            groupName: sample.name,
            initialView: sample
        )
    }

    /// The row Home and Profile draw for a cabal the member is in.
    private var sampleRow: some View {
        CabalPositionRow(
            groupId: sample.id,
            name: sample.name,
            potValueUsd: sample.resolvedPotTotalUsd,
            figures: CabalPositionRowFigures(
                equityUsd: sample.you.equityUsd,
                dollarPnl: sample.you.dollarPnl,
                percentReturn: sample.you.percentReturn
            ),
            isLast: true
        )
    }

    // MARK: - list entry (Home "Your cabals" row, Cabals strip card)

    /// Mirrors `HomePositionsSection` / `CabalsStripSection`: a destination-style
    /// `NavigationLink` inside a lazy container.
    private var listRoot: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader("Your cabals")
                    .padding(.horizontal, MonacoTheme.Space.m)
                MonacoGroupedList {
                    NavigationLink {
                        cabalScreen
                    } label: {
                        sampleRow
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("nav-sample-list-row")
                }
            }
            .padding(.top, MonacoTheme.Space.s)
        }
        .monacoCanvas()
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.large)
        .accessibilityIdentifier("nav-sample-root")
    }

    // MARK: - create entry (Cabals tab → New cabal → Create → cabal screen)

    /// Mirrors `CabalsTabView`: the tab root owns a `navigationDestination(item:)` that
    /// pushes the create form, which pushes the cabal screen.
    private var createRoot: some View {
        ScrollView {
            MonacoGroupedList {
                Button {
                    discoveryRoute = .create
                } label: {
                    MonacoRow(title: "Start a cabal", subtitle: "Name it and set the rules", chevron: true, isLast: true) {
                        SunkenGlyphMark(systemImage: "plus", size: 40)
                    }
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("nav-sample-start-create")
            }
            .padding(.top, MonacoTheme.Space.s)
        }
        .monacoCanvas()
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.large)
        .accessibilityIdentifier("nav-sample-root")
        .navigationDestination(item: $discoveryRoute) { _ in
            CreateGroupSampleForm(cabalScreen: { cabalScreen })
        }
    }

    // MARK: - The screens around the cabal screen

    @ViewBuilder
    private var screensRoot: some View {
        if entry.opensTheBot {
            botsRoot
        } else {
            cabalsRoot
        }
    }

    /// Mirrors the Cabals tab's root: the member's cabals and the "+" that opens the New
    /// cabal sheet.
    private var cabalsRoot: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader("Your cabals")
                    .padding(.horizontal, MonacoTheme.Space.m)
                MonacoGroupedList {
                    NavigationLink(value: GroupNavSampleScreen.cabal(id: sample.id, name: sample.name, isNew: false)) {
                        sampleRow
                    }
                    .buttonStyle(.monacoRow)
                }
            }
            .padding(.top, MonacoTheme.Space.s)
        }
        .monacoCanvas()
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    showNewCabal = true
                } label: {
                    Image(systemName: "plus")
                        .monacoToolbarIcon()
                        .frame(width: 44, height: 44)
                }
                .accessibilityLabel("New cabal")
            }
        }
        .sheet(isPresented: $showNewCabal, onDismiss: pushScreenAfterSheet) {
            NewCabalSheet(
                onCreate: {
                    screenAfterSheet = .start
                    showNewCabal = false
                },
                onJoin: {
                    screenAfterSheet = .joinCode
                    showNewCabal = false
                }
            )
        }
        .task {
            guard entry == .newCabal, !didOpenNewCabal else { return }
            didOpenNewCabal = true
            showNewCabal = true
        }
    }

    /// The bot's row in each state it can be in, as the cabal screen draws it.
    private var botsRoot: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                ForEach(TradingBotSample.allCases, id: \.self) { bot in
                    AgentSectionView(agent: bot.agent)
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .monacoCanvas()
        .navigationTitle("Trading bot")
        .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder
    private func destination(_ screen: GroupNavSampleScreen) -> some View {
        switch screen {
        case .start:
            CreateGroupView(auth: auth, actions: actions) { created in
                replaceTop(with: .cabal(id: created.groupId, name: created.name, isNew: true))
            }
        case .joinCode:
            JoinGroupView(auth: auth, actions: actions) { groupId, name in
                replaceTop(with: .cabal(id: groupId, name: name ?? GroupNavSampleData.name(forCabal: groupId), isNew: false))
            }
        case let .join(cabal):
            JoinGroupView(
                auth: auth,
                groupId: cabal.id,
                groupName: cabal.name,
                joinMode: cabal.mode,
                memberCount: cabal.members,
                actions: actions
            ) { groupId, name in
                replaceTop(with: .cabal(id: groupId, name: name ?? cabal.name, isNew: false))
            }
        case let .cabal(id, name, isNew):
            GroupDetailView(
                auth: auth,
                groupId: id,
                groupName: name,
                initialView: GroupNavSampleData.view(id: id, name: name, isNew: isNew)
            )
        case .bot(let bot):
            AgentDetailView(agent: bot.agent)
        }
    }

    /// Replaces the form with what it produced, as the Cabals tab does, so Back from a new or
    /// joined cabal lands on the root rather than on a form that is still armed.
    private func replaceTop(with screen: GroupNavSampleScreen) {
        if path.isEmpty {
            path = [screen]
        } else {
            path[path.count - 1] = screen
        }
    }

    private func pushScreenAfterSheet() {
        guard let screenAfterSheet else { return }
        self.screenAfterSheet = nil
        path.append(screenAfterSheet)
    }
}

/// A screen the harness pushes over its Cabals root.
private enum GroupNavSampleScreen: Hashable {
    case start
    case joinCode
    case join(GroupNavSampleData.Discovered)
    case cabal(id: String, name: String, isNew: Bool)
    case bot(TradingBotSample)
}

private extension GroupNavSampleEntry {
    /// The three chains the navigation regression covers keep the plain stack they were
    /// written against; everything else pushes onto a path.
    var isNavigationChain: Bool {
        [.root, .list, .create].contains(self)
    }

    var opensTheBot: Bool {
        [.bots, .bot, .botRemoved].contains(self)
    }

    var initialPath: [GroupNavSampleScreen] {
        switch self {
        case .start: [.start]
        case .joinCode: [.joinCode]
        case .join: [.join(GroupNavSampleData.openCabal)]
        case .askToJoin: [.join(GroupNavSampleData.approvalCabal)]
        case .bot: [.bot(.active)]
        case .botRemoved: [.bot(.removed)]
        case .root, .list, .create, .newCabal, .bots: []
        }
    }
}

/// The bot in each state the server can report.
private enum TradingBotSample: CaseIterable, Hashable {
    case active
    case paused
    case removed

    var agent: GroupAgentDTO {
        switch self {
        case .active:
            GroupAgentDTO(
                id: "a1", status: "active", agentDisplayName: "Scout", allocationUsdcMicros: "100000000",
                apiKey: "monaco_ak_7hq2kx9m4c8pzrt3vw5yb6nd2fj8ksue"
            )
        case .paused:
            GroupAgentDTO(
                id: "a2", status: "paused", agentDisplayName: "Night owl", allocationUsdcMicros: "250000000",
                apiKey: "monaco_ak_m3xt8qz2p6wvk4bc9rhy5dn7ja2gfs8u"
            )
        case .removed:
            // The server stops sending the key once a vote removes the bot.
            GroupAgentDTO(id: "a3", status: "revoked", agentDisplayName: "Momentum", allocationUsdcMicros: "50000000")
        }
    }
}

private enum GroupNavSampleData {
    /// What a board or search row knows about a cabal the viewer is not in.
    struct Discovered: Hashable {
        let id: String
        let name: String
        let mode: GroupJoinMode
        let members: Int
    }

    /// The same two cabals `CabalsTabSampleData` lists, so its join stub answers for each the
    /// way its policy says: Dorm 4B fund lets anyone in, Tesla or bust asks its admin.
    static let openCabal = Discovered(id: "5b1f0c9e-0004-4c55-9a51-000000000004", name: "Dorm 4B fund", mode: .open, members: 9)
    static let approvalCabal = Discovered(id: "5b1f0c9e-0005-4c55-9a51-000000000005", name: "Tesla or bust", mode: .request, members: 5)

    static func name(forCabal id: String) -> String {
        CabalsTabSampleData.cabals.first { $0.id == id }?.name ?? GroupDetailSampleData.view.name
    }

    /// The sample cabal under the name the flow arrived with. A cabal that was just started has
    /// an empty pot and one member.
    static func view(id: String, name: String, isNew: Bool) -> GroupViewDTO {
        let base = isNew ? GroupDetailSampleData.emptyView : GroupDetailSampleData.view
        return GroupViewDTO(
            id: id,
            name: name,
            treasuryAddress: base.treasuryAddress,
            potTotalUsd: base.potTotalUsd,
            pot: base.pot,
            you: base.you,
            members: base.members,
            proposals: base.proposals,
            agent: base.agent,
            isCreator: isNew
        )
    }
}

/// Mirrors `CreateGroupView`: a form that owns the pushed-cabal item, so the cabal screen
/// arrives through a `navigationDestination(item:)` that is itself inside a pushed screen.
private struct CreateGroupSampleForm<Cabal: View>: View {
    @ViewBuilder var cabalScreen: () -> Cabal

    private struct CreatedStub: Identifiable, Hashable {
        let id: String
    }

    @State private var createdCabal: CreatedStub?

    var body: some View {
        MonacoTheme.canvas
            .ignoresSafeArea()
            .navigationTitle("Start a cabal")
            .navigationBarTitleDisplayMode(.inline)
            .safeAreaInset(edge: .bottom) {
                BottomCTA {
                    Button("Create cabal") {
                        createdCabal = CreatedStub(id: "created")
                    }
                    .buttonStyle(.monacoPrimary)
                    .accessibilityIdentifier("nav-sample-create-submit")
                }
            }
            .navigationDestination(item: $createdCabal) { _ in
                cabalScreen()
            }
    }
}
#endif
