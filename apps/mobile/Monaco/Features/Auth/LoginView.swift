import SwiftUI

/// Brand block, then SMS or email one-time-code sign-in.
struct LoginView: View {
    @ObservedObject var auth: DynamicAuthService

    @State private var selectedMethod: LoginMethod

    private enum LoginMethod: String, CaseIterable, Identifiable {
        case sms = "Text message"
        case email = "Email"

        var id: String { rawValue }
    }

    init(auth: DynamicAuthService) {
        self.auth = auth
        let settings = Config.dynamic
        if settings.smsLoginEnabled {
            _selectedMethod = State(initialValue: .sms)
        } else {
            _selectedMethod = State(initialValue: .email)
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.xl) {
                LaunchScreenView()
                    .padding(.top, MonacoTheme.Space.xl)

                if let reason = auth.lastSignOutReason {
                    Text(reason)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.destructive)
                        .accessibilityIdentifier("signOutReasonNotice")
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.m) {
                    if showsMethodPicker {
                        MonacoSegmented(availableMethods, selection: $selectedMethod) { $0.rawValue }
                            .accessibilityLabel("Sign-in method")
                            // Switching method mid-send would leave the in-flight request to
                            // report success against the other form: an email screen showing
                            // a code step whose code was texted to a phone number.
                            .disabled(auth.flow.isBusy)
                    }

                    loginContent
                }
                .monacoFullWidthButtons()
            }
            .padding(.horizontal, MonacoTheme.Space.gutter)
            .padding(.bottom, MonacoTheme.Space.l)
        }
        .scrollDismissesKeyboard(.interactively)
        .authScreenBackground()
        .tint(MonacoTheme.accent)
        .foregroundStyle(MonacoTheme.primaryText)
        .onChange(of: selectedMethod) { _, _ in
            auth.resetLoginFlow()
        }
    }

    @ViewBuilder
    private var loginContent: some View {
        switch effectiveMethod {
        case .sms:
            SMSLoginView(auth: auth)
        case .email:
            EmailLoginView(auth: auth)
        }
    }

    private var availableMethods: [LoginMethod] {
        var methods: [LoginMethod] = []
        if Config.dynamic.smsLoginEnabled {
            methods.append(.sms)
        }
        if Config.dynamic.emailLoginEnabled {
            methods.append(.email)
        }
        return methods
    }

    private var showsMethodPicker: Bool {
        availableMethods.count > 1
    }

    private var effectiveMethod: LoginMethod {
        if availableMethods.contains(selectedMethod) {
            return selectedMethod
        }
        return availableMethods.first ?? .sms
    }
}

#Preview {
    LoginView(auth: DynamicAuthService())
}
