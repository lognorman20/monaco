//
//  MonacoApp.swift
//  Monaco
//

import MonacoCore
import os
import SwiftUI

@main
struct MonacoApp: App {
    @StateObject private var auth = PrivyAuthService()

    init() {
        // Resolve the API environment before any UI so a misconfigured build fails at launch.
        let api = Config.api
        AppLogger.session.info("API environment: \(api.debugSummary, privacy: .public)")
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
