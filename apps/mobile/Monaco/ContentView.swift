import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var auth: PrivyAuthService

    var body: some View {
        NavigationStack {
            AuthGateView(auth: auth)
                .padding()
        }
    }
}

#Preview {
    ContentView()
        .environmentObject(PrivyAuthService())
}
