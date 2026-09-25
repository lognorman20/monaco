#if DEBUG
import Combine
import MonacoCore
import SwiftUI

/// Debug-only: the way in (sign-in, the restore, the session gate) against canned state, so QA
/// can screenshot every step of it without Privy or a backend. Launch with
/// `-MonacoWelcomeSample <scenario>`; the bare flag means `code`.
///
/// The sign-in scenarios drive the real `LoginView` through `SampleSignIn`, a canned `OTPSignIn`:
/// sending a code succeeds after a beat and every code is turned down, so the screen stays put to
/// be looked at. The gate scenarios draw the gate's own restoring, loading and failure views.
enum WelcomeSampleScenario: String, CaseIterable {
    /// Back on login after the session ended, with the reason over the form.
    case signedOut
    /// A code went to a phone number; the code field is empty.
    case code
    /// A code went to an email address.
    case emailCode
    /// The six digits typed in were turned down.
    case codeRejected
    /// Launch, restoring a saved sign-in: the launch mark, held.
    case restoring
    /// The saved sign-in couldn't be checked (offline).
    case restoreFailed
    /// Signed in, and the backend session is opening: Home's shape.
    case gateLoading
    /// The backend session wouldn't open.
    case gateFailed
    /// `EmptyState` where the app puts it: between a section's rules, and under a bare header.
    case emptyStates

    static let launchArgument = "-MonacoWelcomeSample"

    static var requested: WelcomeSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: launchArgument) else { return nil }
        guard arguments.indices.contains(flag + 1),
              let scenario = WelcomeSampleScenario(rawValue: arguments[flag + 1])
        else { return .code }
        return scenario
    }
}

struct WelcomeSampleHarness: View {
    let scenario: WelcomeSampleScenario
    @StateObject private var signIn: SampleSignIn

    init(scenario: WelcomeSampleScenario) {
        self.scenario = scenario
        _signIn = StateObject(wrappedValue: SampleSignIn(scenario: scenario))
    }

    var body: some View {
        switch scenario {
        case .signedOut, .code:
            LoginView(auth: signIn, methods: [.sms, .email])
        case .codeRejected:
            LoginView(auth: signIn, methods: [.sms, .email], initialCode: "465354")
        case .emailCode:
            LoginView(auth: signIn, methods: [.sms, .email], initialMethod: .email)
        case .restoring:
            SessionRestoringView()
        case .restoreFailed:
            SessionFailureView(
                title: SessionGateCopy.restoreFailedTitle,
                message: LoginFailureCopy.restoreOffline,
                onRetry: {},
                onSignOut: {}
            )
        case .gateLoading:
            SessionGateSkeleton()
        case .gateFailed:
            SessionFailureView(
                title: SessionGateCopy.openFailedTitle,
                message: "Can't reach Monaco. Check your connection and try again.",
                detail: "URLError notConnectedToInternet\nlocalhost:8080",
                onRetry: {},
                onSignOut: {}
            )
        case .emptyStates:
            EmptyStateSamples()
        }
    }
}

/// A canned `OTPSignIn`. Sending a code always goes through after a beat; checking one always
/// fails as a wrong code, so a sample never leaves the login screen.
final class SampleSignIn: OTPSignIn {
    @Published private(set) var flow: LoginFlow
    @Published private(set) var lastSignOutReason: String?

    init(scenario: WelcomeSampleScenario) {
        switch scenario {
        case .signedOut:
            flow = LoginFlow()
            lastSignOutReason = LoginFailureCopy.sessionExpired
        case .code:
            flow = SampleSignIn.onCodeStep(sentTo: "+15551234567")
        case .emailCode:
            flow = SampleSignIn.onCodeStep(sentTo: "logan.norman@example.com")
        case .codeRejected:
            var rejected = SampleSignIn.onCodeStep(sentTo: "+15551234567")
            _ = rejected.beginVerify()
            rejected.verifyFailed(message: OTPCode.rejectedMessage)
            flow = rejected
        case .restoring, .restoreFailed, .gateLoading, .gateFailed, .emptyStates:
            flow = LoginFlow()
        }
    }

    func sendSMSCode(to phoneNumberE164: String) async {
        await sendCode(to: phoneNumberE164)
    }

    func loginWithSMSCode(_ code: String, sentTo phoneNumberE164: String) async {
        await turnDownCode()
    }

    func sendEmailCode(to email: String) async {
        await sendCode(to: email)
    }

    func loginWithEmailCode(_ code: String, sentTo email: String) async {
        await turnDownCode()
    }

    func resetLoginFlow() {
        flow.returnToAddressEntry()
    }

    private func sendCode(to destination: String) async {
        guard flow.beginSend() else { return }
        lastSignOutReason = nil
        try? await Task.sleep(for: .milliseconds(600))
        flow.sendSucceeded(destination: destination)
    }

    private func turnDownCode() async {
        guard flow.beginVerify() else { return }
        try? await Task.sleep(for: .milliseconds(600))
        flow.verifyFailed(message: OTPCode.rejectedMessage)
    }

    private static func onCodeStep(sentTo destination: String) -> LoginFlow {
        var flow = LoginFlow()
        _ = flow.beginSend()
        flow.sendSucceeded(destination: destination)
        return flow
    }
}

/// `EmptyState` in the places the app uses it, to check it against the rules around it.
private struct EmptyStateSamples: View {
    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                    section("Holdings") {
                        MonacoGroupedList {
                            EmptyState(
                                title: "Nothing bought yet",
                                message: "Add money, then propose the first buy.",
                                actionTitle: "Add money"
                            ) {}
                        }
                    }
                    section("Top cabals") {
                        MonacoGroupedList {
                            EmptyState(
                                title: "No cabal has put money in yet",
                                message: "The first one to fund takes the top spot."
                            )
                        }
                    }
                    section("Your cabals") {
                        EmptyState(
                            title: "No cabals yet",
                            message: "Start one with friends or join an open one.",
                            actionTitle: "Browse cabals"
                        ) {}
                    }
                }
                .padding(.vertical, MonacoTheme.Space.m)
            }
            .monacoCanvas()
            .navigationTitle("Empty states")
        }
    }

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(title)
                .padding(.horizontal, MonacoTheme.Space.m)
            content()
        }
    }
}
#endif
