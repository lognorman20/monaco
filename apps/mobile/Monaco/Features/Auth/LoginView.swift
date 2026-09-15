import SwiftUI

/// M1 login shell: pick SMS or email OTP when both are enabled in config.
struct LoginView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var selectedMethod: LoginMethod

    private enum LoginMethod: String, CaseIterable, Identifiable {
        case sms = "SMS"
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
        VStack(alignment: .leading, spacing: 20) {
            if showsMethodPicker {
                Picker("Login method", selection: $selectedMethod) {
                    ForEach(availableMethods) { method in
                        Text(method.rawValue).tag(method)
                    }
                }
                .pickerStyle(.segmented)
            }

            loginContent
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
