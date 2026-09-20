#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: renders the first-run name screen against canned data so QA can screenshot
/// it and XCUITests can drive it without Privy or a backend. Launch with
/// `-MonacoOnboardingSample [fresh|photo|invalid|failure]`; the bare flag means `fresh`.
///
/// The save is canned, not real: `fresh`/`photo` accept after a short delay, `failure`
/// always rejects so the inline error can be shot. The photo picker is inert here (there is
/// no access token behind it), which is what we want — it still renders the avatar and the
/// camera badge, and the picker itself is covered by the Profile suite.
enum OnboardingSampleScenario: String, CaseIterable {
    /// Brand new account: no name, no photo.
    case fresh
    /// Photo already picked, name still missing.
    case photo
    /// Field prefilled with a draft the rules reject, to shoot inline validation.
    case invalid
    /// Continue always fails, to shoot the inline save error.
    case failure

    static let launchArgument = "-MonacoOnboardingSample"

    static var requested: OnboardingSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument) else { return nil }
        guard arguments.indices.contains(flag + 1),
              let scenario = OnboardingSampleScenario(rawValue: arguments[flag + 1])
        else { return .fresh }
        return scenario
    }

    var initialDraft: String {
        self == .invalid ? "                    " : ""
    }
}

struct OnboardingSampleHarness: View {
    let scenario: OnboardingSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session: AppSessionStore

    init(scenario: OnboardingSampleScenario, auth: PrivyAuthService) {
        self.scenario = scenario
        self.auth = auth
        _session = State(initialValue: Self.makeSession(for: scenario))
    }

    var body: some View {
        OnboardingNameView(
            auth: auth,
            save: save,
            signOut: {},
            initialDraft: scenario.initialDraft
        )
        .environment(session)
    }

    /// Stands in for `AppSessionStore.updateDisplayName`. Keeps the harness on screen after
    /// a success (there is no gate above it) so the saved state can be inspected.
    private func save(_ draft: String) async -> ProfileSaveOutcome {
        try? await Task.sleep(for: .milliseconds(400))
        if scenario == .failure {
            return .failed("Too many changes. Try again in a minute.")
        }
        guard case .success(let normalized) = DisplayNameRules.normalize(draft) else {
            return .failed("Display name is required.")
        }
        session.me = session.me?.withDisplayName(normalized)
        return .saved
    }

    private static func makeSession(for scenario: OnboardingSampleScenario) -> AppSessionStore {
        let session = AppSessionStore()
        session.isLoading = false
        session.me = MeResponse(
            userId: "sample-user",
            displayName: "",
            memberWalletAddress: "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU",
            profilePhotoUrl: scenario == .photo ? ProfileSampleHarness.samplePhotoURL()?.absoluteString : nil,
            createdAt: Date()
        )
        return session
    }
}
#endif
