import DynamicSDKSwift
import SwiftUI

@main
struct MonacoApp: App {
    @StateObject private var auth = DynamicAuthService()

    init() {
        MonacoAppearance.configureUIKit()
        MonacoLaunchTrace.markSceneReady()
        if Config.dynamic.isConfigured {
            _ = DynamicAuthService.ensureSDK(environmentID: Config.dynamic.environmentID)
        }
    }

    var body: some Scene {
        WindowGroup {
            // The root tint is `brand`, not ink. Ink was the bug: it silently beat the brand-blue
            // tab-bar appearance configured in `MonacoAppearance`, so the app's one accent colour
            // was missing from its most persistent chrome. Deleting it outright was half the fix —
            // it also dropped the fallback for everything presented outside `MainTabView`,
            // including the auth SDK's own sheets, which then fell back to system blue `#007AFF`,
            // a few degrees off `#1652F0`. Blue means tap, so the root says brand and the places
            // that need a caret rather than an accent take `MonacoTheme.controlTint` at their own
            // site — `MainTabView` does exactly that for all four tab stacks.
            root
                .tint(MonacoTheme.brand)
                .environmentObject(auth)
        }
    }

    @ViewBuilder
    private var root: some View {
        #if DEBUG
        if MonacoDesignGallery.isEnabled {
            MonacoDesignGallery.rootView()
        } else if TabShellSample.isEnabled {
            // The tab shell has to be entered above `ContentView`: every other harness replaces a
            // screen inside the shell, and this one *is* the shell.
            TabShellSample.rootView(auth: auth)
        } else if ChatSampleQA.isEnabled {
            ChatSampleQA.rootView()
        } else {
            ContentView()
        }
        #else
        ContentView()
        #endif
    }
}
