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
            ContentView()
                .environmentObject(auth)
                .tint(MonacoTheme.ink)
        }
    }
}
