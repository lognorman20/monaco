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
            root
                .environmentObject(auth)
                .tint(MonacoTheme.ink)
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
