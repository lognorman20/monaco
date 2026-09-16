import SwiftUI

/// Email OTP sign-in via Privy — kept for internal testers.
struct EmailLoginView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var emailAddress = ""
    @State private var otpCode = ""
    @FocusState private var focusedField: Field?

    private enum Field {
        case email
        case code
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("We’ll email you a one-time code. Check spam if it doesn’t arrive.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            TextField("Email address", text: $emailAddress)
                .keyboardType(.emailAddress)
                .textContentType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .email)
                .accessibilityIdentifier("emailAddressField")

            if showsOTPField {
                TextField("6-digit code", text: $otpCode)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .focused($focusedField, equals: .code)
                    .accessibilityIdentifier("emailCodeField")
            }

            if let message = statusMessage {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(statusColor)
            }

            HStack {
                if showsOTPField {
                    Button("Continue") {
                        Task {
                            await auth.loginWithEmailCode(otpCode, sentTo: normalizedEmail)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isVerifyDisabled)
                    .accessibilityIdentifier("emailVerifyButton")
                } else {
                    Button("Send code") {
                        Task {
                            await auth.sendEmailCode(to: normalizedEmail)
                            if case .awaitingCode = auth.phase {
                                focusedField = .code
                            }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isSendDisabled)
                    .accessibilityIdentifier("emailSendCodeButton")
                }
            }
        }
    }

    private var normalizedEmail: String {
        emailAddress.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var showsOTPField: Bool {
        switch auth.phase {
        case .awaitingCode, .verifyingCode, .authenticated:
            true
        case .idle, .sendingCode, .failed:
            false
        }
    }

    private var isSendDisabled: Bool {
        normalizedEmail.isEmpty || !normalizedEmail.contains("@") || auth.phase == .sendingCode
    }

    private var isVerifyDisabled: Bool {
        otpCode.trimmingCharacters(in: .whitespacesAndNewlines).count < 4 || auth.phase == .verifyingCode
    }

    private var statusMessage: String? {
        switch auth.phase {
        case .idle:
            return nil
        case .sendingCode:
            return "Sending code…"
        case .awaitingCode:
            return "Enter the code from your email."
        case .verifyingCode:
            return "Signing you in…"
        case .authenticated:
            return "Signed in."
        case .failed(let message):
            return message
        }
    }

    private var statusColor: Color {
        switch auth.phase {
        case .failed:
            return .orange
        case .authenticated:
            return .green
        default:
            return .secondary
        }
    }
}

#Preview {
    EmailLoginView(auth: PrivyAuthService())
}
