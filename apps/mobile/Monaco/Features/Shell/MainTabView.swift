import SwiftUI

/// The four tab roots. Account actions (withdraw, advanced, sign out) live on Profile.
enum MainTab: Hashable {
    case home, cabals, stocks, profile
}

/// Post-auth frame. Tab chrome only — screens live in their feature folders.
struct MainTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @State private var selectedTab: MainTab = .home

    var body: some View {
        TabView(selection: $selectedTab) {
            NavigationStack {
                HomeView(auth: auth, selectedTab: $selectedTab)
            }
            .tabItem {
                Label("Home", systemImage: "house")
                    .accessibilityIdentifier("tab-home")
            }
            .tag(MainTab.home)
            .environment(\.hostMainTab, .home)

            NavigationStack {
                CabalsTabView(auth: auth)
            }
            .tabItem {
                Label("Cabals", systemImage: "person.3")
                    .accessibilityIdentifier("tab-cabals")
            }
            .tag(MainTab.cabals)
            .environment(\.hostMainTab, .cabals)

            NavigationStack {
                AssetsTabView(auth: auth)
            }
            .tabItem {
                Label("Stocks", systemImage: "chart.line.uptrend.xyaxis")
                    .accessibilityIdentifier("tab-assets")
            }
            .tag(MainTab.stocks)
            .environment(\.hostMainTab, .stocks)

            NavigationStack {
                ProfileTabView(auth: auth)
            }
            .tabItem {
                Label("Profile", systemImage: "person.crop.circle")
                    .accessibilityIdentifier("tab-profile")
            }
            .tag(MainTab.profile)
            .environment(\.hostMainTab, .profile)
        }
        .tint(MonacoTheme.ink)
        // Each stack knows its tab (`hostMainTab`) and which one is showing, so screens in a tab
        // the member switched away from stop polling. See `pollWhileVisible`.
        .environment(\.selectedMainTab, selectedTab)
        .onChange(of: selectedTab) { _, _ in
            Haptics.selection()
        }
        // lane: notifications
        .pushPermissionPrompt()
    }
}
