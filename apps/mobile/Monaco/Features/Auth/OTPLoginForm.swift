import SwiftUI
import UIKit

/// One-time-code sign-in: an address field, then the code field, then in. SMS and email
/// differ only in the address they ask for, so both run through this one form — the code
/// rules (6 digits, digits only, auto-submit) live in a single place.
struct OTPLoginForm: View {
    /// What the form asks for before it can send a code.
    struct Destination {
        let caption: String
        let prompt: String
        let keyboardType: UIKeyboardType
        let contentType: UITextContentType
        /// Shown under the field once what's typed can't be sent to. Given what was typed,
        /// so it can say why rather than repeat the same instruction.
        let invalidHint: (String) -> String
        let changeLabel: String
        let addressFieldIdentifier: String
        let identifierPrefix: String
        /// The address in the shape the provider needs, or nil when it isn't one yet.
        let normalize: (String) -> String?
        /// How the address reads back in "we sent a code to …".
        let display: (String) -> String
    }

    @ObservedObject var auth: DynamicAuthService
    let destination: Destination
    let send: (String) async -> Void
    let verify: (_ code: String, _ sentTo: String) async -> Void

    @State private var address = ""
    @State private var otpCode = ""
    @FocusState private var focusedField: Field?

    private static let codeLength = 6

    private enum Field {
        case address
        case code
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(destination.caption)
                .authSecondaryCaption()

            TextField(
                "",
                text: $address,
                prompt: Text(destination.prompt).foregroundStyle(MonacoTheme.Ink.fgSubtle)
            )
                .keyboardType(destination.keyboardType)
                .textContentType(destination.contentType)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .focused($focusedField, equals: .address)
                .authTextFieldStyle()
                .disabled(showsCodeField)
                .accessibilityIdentifier(destination.addressFieldIdentifier)
                .onChange(of: address) { _, _ in
                    // A failed first send leaves its message under a field the member is
                    // now correcting; editing is the answer to it, so clear it. Never on the
                    // code step: the field is locked there and the failure is about the code.
                    if case .failed = auth.phase, !auth.loginStep.isCodeEntry {
                        auth.resetLoginFlow()
                    }
                }

            if showsAddressHint {
                Text(destination.invalidHint(address))
                    .font(.footnote)
                    .foregroundStyle(MonacoTheme.Ink.fgMuted)
                    .accessibilityIdentifier("\(destination.identifierPrefix)AddressHint")
            }

            if showsCodeField {
                TextField(
                    "",
                    text: $otpCode,
                    prompt: Text("6-digit code").foregroundStyle(MonacoTheme.Ink.fgSubtle)
                )
                    .keyboardType(.numberPad)
                    .textContentType(.oneTimeCode)
                    .focused($focusedField, equals: .code)
                    .authTextFieldStyle()
                    .accessibilityIdentifier("\(destination.identifierPrefix)CodeField")
                    .onChange(of: otpCode) { _, newValue in
                        codeChanged(to: newValue)
                    }
            }

            if let message = statusMessage {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(statusColor)
            }

            HStack {
                if showsCodeField {
                    Button("Continue") {
                        Task { await submitCode(otpCode) }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(isVerifyDisabled)
                    .accessibilityIdentifier("\(destination.identifierPrefix)VerifyButton")
                } else {
                    Button("Send code") {
                        Task { await sendCode() }
                    }
                    .buttonStyle(.monacoPrimary)
                    .disabled(isSendDisabled)
                    .accessibilityIdentifier("\(destination.identifierPrefix)SendCodeButton")
                }
            }

            if showsCodeField {
                HStack {
                    Button("Send a new code") {
                        Task { await resendCode() }
                    }
                    .buttonStyle(.plain)
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 44)
                    .foregroundStyle(MonacoTheme.Ink.accent)
                    .disabled(auth.flow.isBusy)
                    .accessibilityIdentifier("\(destination.identifierPrefix)ResendCodeButton")

                    Spacer()

                    Button(destination.changeLabel) {
                        otpCode = ""
                        auth.resetLoginFlow()
                    }
                    .buttonStyle(.plain)
                    .font(.footnote.weight(.semibold))
                    .frame(minHeight: 44)
                    .foregroundStyle(MonacoTheme.Ink.fgMuted)
                    .disabled(auth.flow.isBusy)
                    .accessibilityIdentifier("\(destination.identifierPrefix)ChangeAddressButton")
                }
            }
        }
    }

    // MARK: Actions

    private func sendCode() async {
        guard let normalized = destination.normalize(address) else { return }
        await send(normalized)
        if case .awaitingCode = auth.phase {
            focusedField = .code
        }
    }

    /// Keeps whatever the member has typed unless the new code actually went out — a
    /// throttled resend used to wipe the field it was about to be typed into.
    private func resendCode() async {
        guard let sentTo = sentDestination else { return }
        await send(sentTo)
        if case .awaitingCode = auth.phase {
            otpCode = ""
            focusedField = .code
        }
    }

    private func codeChanged(to newValue: String) {
        let sanitized = String(newValue.filter(\.isASCIIDigit).prefix(Self.codeLength))
        if sanitized != newValue {
            otpCode = sanitized
        }
        // Autofill drops all six digits in at once: don't make them tap Continue too.
        guard sanitized.count == Self.codeLength, !auth.flow.isBusy else { return }
        Task { await submitCode(sanitized) }
    }

    private func submitCode(_ code: String) async {
        guard code.count == Self.codeLength, !auth.flow.isBusy, let sentTo = sentDestination else { return }
        await verify(code, sentTo)
    }

    // MARK: Derived state

    /// The address the code actually went to, not whatever is in the field now.
    private var sentDestination: String? {
        auth.loginStep.destination ?? destination.normalize(address)
    }

    private var showsCodeField: Bool {
        if case .authenticated = auth.phase { return true }
        return auth.loginStep.isCodeEntry
    }

    private var showsAddressHint: Bool {
        !showsCodeField
            && !address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && destination.normalize(address) == nil
    }

    private var isSendDisabled: Bool {
        destination.normalize(address) == nil || auth.flow.isBusy
    }

    private var isVerifyDisabled: Bool {
        otpCode.count != Self.codeLength || auth.flow.isBusy
    }

    private var statusMessage: String? {
        switch auth.phase {
        case .idle, .restoring, .restoreFailed, .authenticated:
            return nil
        case .sendingCode:
            return "Sending code…"
        case .awaitingCode:
            guard let sentTo = auth.loginStep.destination else { return nil }
            return "Enter the 6-digit code we sent to \(destination.display(sentTo))."
        case .verifyingCode:
            return "Signing you in…"
        case .failed(let message):
            return message
        }
    }

    private var statusColor: Color {
        // Green means profit and nothing else (§0 rule 1), so there is no success colour here:
        // a signed-in status line is muted ink like every other non-failure phase.
        switch auth.phase {
        case .failed:
            return MonacoTheme.lossOnHero
        default:
            return MonacoTheme.Ink.fgMuted
        }
    }
}

private extension Character {
    /// `isNumber` also matches "½" and non-Latin digits, which no OTP field wants.
    var isASCIIDigit: Bool {
        isASCII && isNumber
    }
}
