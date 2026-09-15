//
//  MonacoApp.swift
//  Monaco
//

import SwiftUI

@main
struct MonacoApp: App {
    @StateObject private var auth = PrivyAuthService()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(auth)
        }
    }
}
