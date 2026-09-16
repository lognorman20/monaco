import SwiftUI

/// SMS OTP sign-in via Privy — primary demo path.
struct SMSLoginView: View {
    @ObservedObject var auth: PrivyAuthService

    @State private var phoneNumber = ""
    @State private var otpCode = ""
    @FocusState private var focusedField: Field?

    private enum Field {
        case phone
        case code
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("We’ll text you a one-time code to sign in.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            TextField("Phone number", text: $phoneNumber)
                .keyboardType(.phonePad)
                .textContentType(.telephoneNumber)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .phone)
                .accessibilityIdentifier("smsPhoneField")

            if showsOTPField {
                TextField("6-digit code", text: $otpCode)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .focused($focusedField, equals: .code)
                    .accessibilityIdentifier("smsCodeField")
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
                            await auth.loginWithSMSCode(otpCode, sentTo: normalizedPhone)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isVerifyDisabled)
                    .accessibilityIdentifier("smsVerifyButton")
                } else {
                    Button("Send code") {
                        Task {
                            await auth.sendSMSCode(to: normalizedPhone)
                            if case .awaitingCode = auth.phase {
                                focusedField = .code
                            }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isSendDisabled)
                    .accessibilityIdentifier("smsSendCodeButton")
                }
            }
        }
    }

    private var normalizedPhone: String {
        phoneNumber.trimmingCharacters(in: .whitespacesAndNewlines)
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
        normalizedPhone.isEmpty || auth.phase == .sendingCode
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
            return "Enter the code from your text message."
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
    SMSLoginView(auth: PrivyAuthService())
}
