import SwiftUI

/// Post-auth frame. Tab chrome only — screens live in their feature folders.
struct MainTabView: View {
    @ObservedObject var auth: PrivyAuthService

    var body: some View {
        TabView {
            NavigationStack {
                HomeView(auth: auth)
            }
            .tabItem {
                Label("Home", systemImage: "house")
                    .accessibilityIdentifier("tab-home")
            }

            NavigationStack {
                ProfileTabView(auth: auth)
            }
            .tabItem {
                Label("Profile", systemImage: "person")
                    .accessibilityIdentifier("tab-profile")
            }

            NavigationStack {
                CabalsTabView(auth: auth)
            }
            .tabItem {
                Label("Cabals", systemImage: "person.3")
                    .accessibilityIdentifier("tab-cabals")
            }

            NavigationStack {
                AssetsTabView()
            }
            .tabItem {
                Label("Assets", systemImage: "chart.pie")
                    .accessibilityIdentifier("tab-assets")
            }

            NavigationStack {
                SettingsView(auth: auth)
            }
            .tabItem {
                Label("Settings", systemImage: "gearshape")
                    .accessibilityIdentifier("tab-settings")
            }
        }
        .tint(MonacoTheme.ink)
    }
}
