import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var auth: PrivyAuthService

    var body: some View {
        root
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoRootAppearance()
    }

    @ViewBuilder
    private var root: some View {
        #if DEBUG
        if let scenario = ProfileSampleScenario.requested {
            ProfileSampleHarness(scenario: scenario, auth: auth)
        } else if CabalsTabSampleData.isEnabled {
            CabalsTabSampleHarness(auth: auth)
        } else {
            AuthGateView(auth: auth)
        }
        #else
        AuthGateView(auth: auth)
        #endif
    }
}

#Preview {
    ContentView()
        .environmentObject(PrivyAuthService())
}
