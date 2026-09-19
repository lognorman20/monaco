//
//  MonacoApp.swift
//  Monaco
//

import SwiftUI

@main
struct MonacoApp: App {
    @StateObject private var auth = PrivyAuthService()

    init() {
        MonacoAppearance.configureUIKit()
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
        if ChatSampleQA.isEnabled {
            ChatSampleQA.rootView()
        } else {
            ContentView()
        }
        #else
        ContentView()
        #endif
    }
}
