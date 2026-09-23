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
            // No root `.tint`. It used to be ink, which silently beat the brand-blue tab-bar
            // appearance configured in `MonacoAppearance` — so the app's one accent colour was
            // missing from its most persistent chrome. Carets, spinners and pickers now take
            // `MonacoTheme.controlTint` at their own call sites; everything tappable is brand.
            root
                .environmentObject(auth)
        }
    }

    @ViewBuilder
    private var root: some View {
        #if DEBUG
        if MonacoDesignGallery.isEnabled {
            MonacoDesignGallery.rootView()
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
