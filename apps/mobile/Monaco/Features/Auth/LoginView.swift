import SwiftUI

/// What the sign-in screens need from the auth service: the flow they draw and the requests they
/// start. `PrivyAuthService` is the app's. The sample harness hands them a canned one, which is how
/// every step of the form can be screenshotted without Privy.
@MainActor
protocol OTPSignIn: ObservableObject {
    var flow: LoginFlow { get }
    /// Why the member is back on login without having asked to be, when they are.
    var lastSignOutReason: String? { get }
    func sendSMSCode(to phoneNumberE164: String) async
    func loginWithSMSCode(_ code: String, sentTo phoneNumberE164: String) async
    func sendEmailCode(to email: String) async
    func loginWithEmailCode(_ code: String, sentTo email: String) async
    /// "Change number", or switching method: back to the address field.
    func resetLoginFlow()
}

extension PrivyAuthService: OTPSignIn {}

/// The two ways in. Which of them are on comes from the build (`Config.privy`).
enum LoginMethod: String, CaseIterable, Identifiable {
    case sms = "Text message"
    case email = "Email"

    var id: String { rawValue }

    /// The methods turned on, texting first.
    static func available(sms: Bool, email: Bool) -> [LoginMethod] {
        allCases.filter { $0 == .sms ? sms : email }
    }

    static var configured: [LoginMethod] {
        available(sms: Config.privy.smsLoginEnabled, email: Config.privy.emailLoginEnabled)
    }
}

/// Sign-in, as one composition from the top of the screen down: the brand, then the method, the
/// field and the button. The form keeps its button above the keyboard while a field is in use,
/// so the phone pad (which has no return key) never hides the only way forward.
struct LoginView<Auth: OTPSignIn>: View {
    @ObservedObject var auth: Auth

    private let methods: [LoginMethod]
    /// Debug harness only: the code the form opens with, to shoot a typed code.
    private let initialCode: String

    @State private var selectedMethod: LoginMethod

    init(
        auth: Auth,
        methods: [LoginMethod] = LoginMethod.configured,
        initialMethod: LoginMethod? = nil,
        initialCode: String = ""
    ) {
        self.auth = auth
        self.methods = methods
        self.initialCode = initialCode
        _selectedMethod = State(initialValue: initialMethod ?? methods.first ?? .sms)
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    LaunchScreenView()
                    form(scroll: proxy)
                        .padding(.top, 40)
                }
                .padding(.horizontal, MonacoTheme.Space.gutter)
                .padding(.top, 72)
                .padding(.bottom, MonacoTheme.Space.l)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
        }
        .authScreenBackground()
        .tint(MonacoTheme.accent)
        .foregroundStyle(MonacoTheme.primaryText)
        .onChange(of: selectedMethod) { _, _ in
            auth.resetLoginFlow()
        }
    }

    private func form(scroll: ScrollViewProxy) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            // Why the member is here without having signed out: a quiet line over the form,
            // not a banner, gone with the next code they ask for.
            if let reason = auth.lastSignOutReason {
                Text(reason)
                    .font(MonacoTheme.Typo.caption)
                    .foregroundStyle(MonacoTheme.loss)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("signOutReasonNotice")
                    .padding(.bottom, MonacoTheme.Space.sm)
            }

            if methods.count > 1 {
                MonacoSegmented(methods, selection: $selectedMethod) { $0.rawValue }
                    .accessibilityLabel("Sign-in method")
                    // Switching method mid-send would leave the in-flight request to report
                    // success against the other form: an email screen showing a code step
                    // whose code was texted to a phone number.
                    .disabled(auth.flow.isBusy)
                    .padding(.bottom, MonacoTheme.Space.l)
            }

            switch effectiveMethod {
            case .sms:
                SMSLoginView(auth: auth, scroll: scroll, initialCode: initialCode)
            case .email:
                EmailLoginView(auth: auth, scroll: scroll, initialCode: initialCode)
            }
        }
        .monacoFullWidthButtons()
    }

    private var effectiveMethod: LoginMethod {
        methods.contains(selectedMethod) ? selectedMethod : (methods.first ?? .sms)
    }
}

#Preview {
    LoginView(auth: PrivyAuthService())
}
