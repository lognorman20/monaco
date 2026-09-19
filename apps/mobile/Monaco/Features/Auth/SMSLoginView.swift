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
            Text("We’ll text you a code to sign in.")
                .authSecondaryCaption()

            TextField(
                "",
                text: $phoneNumber,
                prompt: Text("Phone number").foregroundStyle(MonacoTheme.tertiaryText)
            )
                .keyboardType(.phonePad)
                .textContentType(.telephoneNumber)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .phone)
                .authTextFieldStyle()
                .accessibilityIdentifier("smsPhoneField")

            if showsOTPField {
                TextField(
                    "",
                    text: $otpCode,
                    prompt: Text("6-digit code").foregroundStyle(MonacoTheme.tertiaryText)
                )
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .focused($focusedField, equals: .code)
                    .authTextFieldStyle()
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
                    .buttonStyle(.monacoPrimary)
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
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSendDisabled)
                    .accessibilityIdentifier("smsSendCodeButton")
                }
            }
        }
    }

    private var normalizedPhone: String {
        let trimmed = phoneNumber.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("+") {
            return trimmed
        }
        let digits = trimmed.filter(\.isNumber)
        if digits.count == 10 {
            return "+1\(digits)"
        }
        if digits.count == 11, digits.first == "1" {
            return "+\(digits)"
        }
        return trimmed
    }

    /// "(555) 123-4567" for US numbers, otherwise what was typed.
    private var displayPhone: String {
        let digits = normalizedPhone.filter(\.isNumber)
        if normalizedPhone.hasPrefix("+1"), digits.count == 11 {
            let d = Array(digits.dropFirst())
            return "(\(String(d[0..<3]))) \(String(d[3..<6]))-\(String(d[6..<10]))"
        }
        return normalizedPhone
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
            return "Enter the 6-digit code we sent to \(displayPhone)."
        case .verifyingCode:
            return "Signing you in…"
        case .authenticated:
            return nil
        case .failed(let message):
            return message
        }
    }

    private var statusColor: Color {
        switch auth.phase {
        case .failed:
            return MonacoTheme.destructive
        case .authenticated:
            return MonacoTheme.success
        default:
            return MonacoTheme.secondaryText
        }
    }
}

#Preview {
    SMSLoginView(auth: PrivyAuthService())
}
