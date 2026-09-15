import SwiftUI

/// SMS OTP login via Privy. Email/password UI lands in M1-T11; email OTP is enabled in config (T10).
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
            Text("Sign in with SMS")
                .font(.title2.bold())

            Text("Use E.164 format, e.g. +14155552671.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            TextField("Phone number", text: $phoneNumber)
                .keyboardType(.phonePad)
                .textContentType(.telephoneNumber)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .phone)

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
                            await auth.loginWithSMSCode(otpCode, sentTo: normalizedPhone)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isVerifyDisabled)
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
            return "Sending SMS code…"
        case .awaitingCode:
            return "Enter the code from your text message."
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
    SMSLoginView(auth: PrivyAuthService())
}
