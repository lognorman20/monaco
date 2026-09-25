import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var auth: PrivyAuthService

    var body: some View {
        root
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .monacoRootAppearance()
            .onAppear { MonacoLaunchTrace.markFirstFrame() }
    }

    @ViewBuilder
    private var root: some View {
        #if DEBUG
        if let scenario = OnboardingSampleScenario.requested {
            OnboardingSampleHarness(scenario: scenario, auth: auth)
        } else if let scenario = WelcomeSampleScenario.requested {
            WelcomeSampleHarness(scenario: scenario)
        } else if let scenario = ProfileSampleScenario.requested {
            ProfileSampleHarness(scenario: scenario, auth: auth)
        } else if let scenario = HomeSampleScenario.requested {
            HomeSampleHarness(scenario: scenario, auth: auth)
        } else if CabalsTabSampleData.isEnabled {
            CabalsTabSampleHarness(auth: auth)
        } else if let entry = GroupNavSampleEntry.requested {
            GroupNavSampleHarness(entry: entry, auth: auth)
        } else if let scenario = GroupDetailSampleScenario.requested {
            GroupDetailSampleHarness(scenario: scenario, auth: auth)
        } else if let scenario = StocksTabSampleScenario.requested {
            StocksTabSampleHarness(scenario: scenario, auth: auth)
        } else if let scenario = AssetDetailSampleScenario.requested {
            AssetDetailSampleHarness(scenario: scenario, auth: auth)
        } else if let scenario = MoneyFlowSampleScenario.requested {
            MoneyFlowSampleHarness(scenario: scenario, auth: auth)
        // lane: portfolio
        } else if let scenario = PortfolioSampleScenario.requested {
            PortfolioSampleHarness(scenario: scenario, auth: auth)
        } else if SampleProposalFeedService.isRequested {
            SampleProposalFeedRoot()
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
