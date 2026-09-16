import SwiftUI

/// Email OTP login via Privy. Privy dashboard uses inbox code, not password.
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
            Text("Sign in with email")
                .font(.title2.bold())

            Text("We send a one-time code to your inbox. Check spam if it does not arrive.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            TextField("Email address", text: $emailAddress)
                .keyboardType(.emailAddress)
                .textContentType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .email)

            if showsOTPField {
                TextField("6-digit code", text: $otpCode)
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .focused($focusedField, equals: .code)
            }

            if let message = statusMessage {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(statusColor)
            }

            if case .authenticated = auth.phase, let token = auth.accessToken {
                Label("Privy access token ready for session API.", systemImage: "checkmark.seal.fill")
                    .font(.footnote)
                    .foregroundStyle(.green)

                Text(tokenPreview(token))
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            HStack {
                if showsOTPField {
                    Button("Verify code") {
                        Task {
                            await auth.loginWithEmailCode(otpCode, sentTo: normalizedEmail)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isVerifyDisabled)
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
                }
            }

            if case .authenticated = auth.phase {
                Button("Sign out") {
                    Task { await auth.logout() }
                }
                .buttonStyle(.bordered)
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
            return "Sending email code…"
        case .awaitingCode:
            return "Enter the code from your email."
        case .verifyingCode:
            return "Verifying code…"
        case .authenticated:
            return "Signed in with Privy."
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

    private func tokenPreview(_ token: String) -> String {
        guard token.count > 16 else { return token }
        return String(token.prefix(8)) + "…" + String(token.suffix(8))
    }
}

#Preview {
    EmailLoginView(auth: PrivyAuthService())
}
