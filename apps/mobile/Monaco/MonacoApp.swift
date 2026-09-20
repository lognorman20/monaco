//
//  MonacoApp.swift
//  Monaco
//

import MonacoCore
import SwiftUI

@main
struct MonacoApp: App {
    @StateObject private var auth = PrivyAuthService()

    init() {
        APITelemetryRegistry.shared.register(APILogTelemetry())
        DiagnosticsSubscriber.shared.start()
        MonacoAppearance.configureUIKit()
        MonacoLaunchTrace.markSceneReady()
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
