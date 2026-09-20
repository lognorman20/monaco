#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the *real* `GroupDetailView` reached the way the app really reaches it,
/// on canned data and with no backend.
///
/// `GroupDetailSampleHarness` only ever shows `GroupDetailContent` at the root of a
/// `NavigationStack`, so it cannot catch a bug in how `GroupDetailView` behaves once the
/// cabal screen is itself a pushed screen. This harness pushes the real `GroupDetailView`
/// through each entry chain the product uses:
///
/// - `root`   — cabal screen at the stack root (baseline).
/// - `list`   — tab root → `NavigationLink` row → cabal screen (Home/Cabals lists).
/// - `create` — tab root → pushed "New cabal" form → cabal screen (`CreateGroupView`).
///
/// Launch with `-MonacoGroupNavSample <entry>`.
enum GroupNavSampleEntry: String, CaseIterable {
    case root
    case list
    case create

    static let launchArgument = "-MonacoGroupNavSample"

    static var requested: GroupNavSampleEntry? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument), arguments.indices.contains(flag + 1) else { return nil }
        return GroupNavSampleEntry(rawValue: arguments[flag + 1])
    }
}

struct GroupNavSampleHarness: View {
    let entry: GroupNavSampleEntry
    @ObservedObject var auth: DynamicAuthService

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

    private enum DiscoveryStub: Identifiable, Hashable {
        case create
        var id: Self { self }
    }

    private var sample: GroupViewDTO { GroupDetailSampleData.view }

    var body: some View {
        // The live app runs every stack inside a `TabView`; keep that here so the harness
        // exercises the same navigation container the bug was reported against.
        TabView {
            NavigationStack {
                stack
            }
            .tabItem {
                Label("Cabals", systemImage: "person.3")
                    .accessibilityIdentifier("tab-cabals")
            }
        }
        .tint(MonacoTheme.ink)
        .environment(session)
    }

    @ViewBuilder
    private var stack: some View {
        switch entry {
        case .root:
            cabalScreen
        case .list:
            listRoot
        case .create:
            createRoot
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

    // MARK: - list entry (Home "Your cabals" row, Cabals strip card)

    /// Mirrors `HomePositionsSection` / `CabalsStripSection`: a destination-style
    /// `NavigationLink` inside a lazy container.
    private var listRoot: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader("Your cabals")
                NavigationLink {
                    cabalScreen
                } label: {
                    MonacoRowCard(
                        systemImage: "person.3",
                        title: sample.name,
                        subtitle: "Your slice $311.50",
                        trailing: nil
                    )
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("nav-sample-list-row")
            }
            .padding(MonacoTheme.Space.m)
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
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                Button("Start a cabal") { discoveryRoute = .create }
                    .buttonStyle(.monacoPrimary)
                    .accessibilityIdentifier("nav-sample-start-create")
            }
            .padding(MonacoTheme.Space.m)
        }
        .monacoCanvas()
        .navigationTitle("Cabals")
        .navigationBarTitleDisplayMode(.large)
        .accessibilityIdentifier("nav-sample-root")
        .navigationDestination(item: $discoveryRoute) { _ in
            CreateGroupSampleForm(cabalScreen: { cabalScreen })
        }
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
        Form {
            Section {
                Button("Create cabal") {
                    createdCabal = CreatedStub(id: "created")
                }
                .accessibilityIdentifier("nav-sample-create-submit")
            }
        }
        .monacoFormScreen()
        .navigationTitle("New cabal")
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $createdCabal) { _ in
            cabalScreen()
        }
    }
}
#endif
