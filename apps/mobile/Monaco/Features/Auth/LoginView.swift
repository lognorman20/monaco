import SwiftUI

/// M5 product launch shell: hero + SMS or email OTP sign-in.
struct LoginView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var selectedMethod: LoginMethod

    private enum LoginMethod: String, CaseIterable, Identifiable {
        case sms = "Text message"
        case email = "Email"

        var id: String { rawValue }
    }

    init(auth: PrivyAuthService) {
        self.auth = auth
        let settings = Config.privy
        if settings.smsLoginEnabled {
            _selectedMethod = State(initialValue: .sms)
        } else {
            _selectedMethod = State(initialValue: .email)
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                LaunchScreenView()

                VStack(alignment: .leading, spacing: 20) {
                    Text("Sign in")
                        .font(.title2.bold())

                    if showsMethodPicker {
                        Picker("Sign-in method", selection: $selectedMethod) {
                            ForEach(availableMethods) { method in
                                Text(method.rawValue).tag(method)
                            }
                        }
                        .pickerStyle(.segmented)
                    }

                    loginContent
                }
            }
            .padding(.vertical, 24)
        }
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
        if Config.privy.smsLoginEnabled {
            methods.append(.sms)
        }
        if Config.privy.emailLoginEnabled {
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
    LoginView(auth: PrivyAuthService())
}
