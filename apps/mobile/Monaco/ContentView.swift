import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var auth: PrivyAuthService

    var body: some View {
        AuthGateView(auth: auth)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoRootAppearance()
    }
}

#Preview {
    ContentView()
        .environmentObject(PrivyAuthService())
}
