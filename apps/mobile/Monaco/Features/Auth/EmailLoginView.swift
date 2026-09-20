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
                .authSecondaryCaption()

            TextField(
                "",
                text: $emailAddress,
                prompt: Text("Email address").foregroundStyle(MonacoTheme.tertiaryText)
            )
                .keyboardType(.emailAddress)
                .textContentType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .email)
                .authTextFieldStyle()
                .disabled(showsOTPField)
                .accessibilityIdentifier("emailAddressField")

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
                    .buttonStyle(.monacoPrimary)
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
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSendDisabled)
                    .accessibilityIdentifier("emailSendCodeButton")
                }
            }

            if showsOTPField {
                HStack {
                    Button("Send a new code") {
                        otpCode = ""
                        Task {
                            await auth.sendEmailCode(to: normalizedEmail)
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
                    .accessibilityIdentifier("emailResendCodeButton")

                    Spacer()

                    Button("Change email") {
                        otpCode = ""
                        auth.resetLoginFlow()
                    }
                    .buttonStyle(.plain)
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 44)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .disabled(auth.phase == .verifyingCode)
                    .accessibilityIdentifier("emailChangeAddressButton")
                }
            }
        }
    }

    private var normalizedEmail: String {
        emailAddress.trimmingCharacters(in: .whitespacesAndNewlines)
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
        normalizedEmail.isEmpty || !normalizedEmail.contains("@") || auth.phase == .sendingCode
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
            return "Enter the 6-digit code we sent to \(normalizedEmail)."
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
    EmailLoginView(auth: PrivyAuthService())
}
