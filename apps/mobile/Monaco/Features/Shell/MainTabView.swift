import SwiftUI

/// Post-auth five-tab shell: Home, Profile, Groups, Assets, Settings.
struct MainTabView: View {
    @ObservedObject var auth: PrivyAuthService
    let home: HomeViewDTO
    let profile: MeResponse
    var onRefresh: () async -> Void = {}

    @State private var selectedTab = MainTab.home

    var body: some View {
        TabView(selection: $selectedTab) {
            NavigationStack {
                HomeView(auth: auth, onRefresh: onRefresh)
            }
            .tabItem {
                Label(MainTab.home.title, systemImage: MainTab.home.systemImage)
            }
            .tag(MainTab.home)
            .accessibilityIdentifier("tab-home")

            NavigationStack {
                ProfileTabView(auth: auth, profile: profile)
            }
            .tabItem {
                Label(MainTab.profile.title, systemImage: MainTab.profile.systemImage)
            }
            .tag(MainTab.profile)
            .accessibilityIdentifier("tab-profile")

            NavigationStack {
                GroupsTabView(auth: auth, home: home, onRefresh: onRefresh)
            }
            .tabItem {
                Label(MainTab.groups.title, systemImage: MainTab.groups.systemImage)
            }
            .tag(MainTab.groups)
            .accessibilityIdentifier("tab-groups")

            NavigationStack {
                AssetsView(auth: auth, home: home, onRefresh: onRefresh)
            }
            .tabItem {
                Label(MainTab.assets.title, systemImage: MainTab.assets.systemImage)
            }
            .tag(MainTab.assets)
            .accessibilityIdentifier("tab-assets")

            NavigationStack {
                SettingsView(
                    auth: auth,
                    memberWalletAddress: profile.memberWalletAddress,
                    treasuryAddress: nil
                )
            }
            .tabItem {
                Label(MainTab.settings.title, systemImage: MainTab.settings.systemImage)
            }
            .tag(MainTab.settings)
            .accessibilityIdentifier("tab-settings")
        }
        .tint(MonacoTheme.accent)
    }
}

private enum MainTab: Hashable {
    case home
    case profile
    case groups
    case assets
    case settings

    var title: String {
        switch self {
        case .home: "Home"
        case .profile: "Profile"
        case .groups: "Groups"
        case .assets: "Assets"
        case .settings: "Settings"
        }
    }

    var systemImage: String {
        switch self {
        case .home: "house"
        case .profile: "person.circle"
        case .groups: "person.3"
        case .assets: "chart.pie"
        case .settings: "gearshape"
        }
    }
}

#Preview {
    MainTabView(
        auth: PrivyAuthService(),
        home: HomeViewDTO(groups: [], people: []),
        profile: MeResponse(userId: "preview", displayName: "Preview", memberWalletAddress: "DemoMemberAddress1111111111111111111111")
    )
    .monacoRootAppearance()
}
