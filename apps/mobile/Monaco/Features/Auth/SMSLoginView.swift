import SwiftUI

/// SMS OTP sign-in — primary demo path.
struct SMSLoginView: View {
    @ObservedObject var auth: DynamicAuthService

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
                .disabled(showsOTPField)
                .accessibilityIdentifier("smsPhoneField")
                .onChange(of: phoneNumber) { _, _ in
                    if case .failed = auth.phase {
                        auth.resetLoginFlow()
                    }
                }

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

            if showsOTPField {
                HStack {
                    Button("Send a new code") {
                        otpCode = ""
                        Task {
                            await auth.sendSMSCode(to: normalizedPhone)
                            if case .awaitingCode = auth.phase {
                                focusedField = .code
                            }
                        }
                    }
                    .buttonStyle(.plain)
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 44)
                    .foregroundStyle(MonacoTheme.accent)
                    .disabled(auth.phase == .verifyingCode)
                    .accessibilityIdentifier("smsResendCodeButton")

                    Spacer()

                    Button("Change number") {
                        otpCode = ""
                        auth.resetLoginFlow()
                    }
                    .buttonStyle(.plain)
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 44)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .disabled(auth.phase == .verifyingCode)
                    .accessibilityIdentifier("smsChangeAddressButton")
                }
            }
        }
    }

    private var normalizedPhone: String {
        LoginPhone.normalizedE164(phoneNumber)
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
        case .awaitingCode, .verifyingCode, .codeRejected, .authenticated:
            true
        case .idle, .sendingCode, .failed, .restoring, .restoreFailed:
            false
        }
    }

    private var isSendDisabled: Bool {
        !LoginPhone.isComplete(phoneNumber) || auth.phase == .sendingCode
    }

    private var isVerifyDisabled: Bool {
        otpCode.trimmingCharacters(in: .whitespacesAndNewlines).count < 4 || auth.phase == .verifyingCode
    }

    private var statusMessage: String? {
        switch auth.phase {
        case .idle, .restoring, .restoreFailed:
            return nil
        case .sendingCode:
            return "Sending code…"
        case .awaitingCode:
            return "Enter the 6-digit code we sent to \(displayPhone)."
        case .verifyingCode:
            return "Signing you in…"
        case .authenticated:
            return nil
        case .failed(let message), .codeRejected(let message):
            return message
        }
    }

    private var statusColor: Color {
        switch auth.phase {
        case .failed, .codeRejected:
            return MonacoTheme.destructive
        case .authenticated:
            return MonacoTheme.success
        default:
            return MonacoTheme.secondaryText
        }
    }
}

#Preview {
    SMSLoginView(auth: DynamicAuthService())
}
