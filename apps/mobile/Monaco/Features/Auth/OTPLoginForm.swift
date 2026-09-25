import MonacoCore
import SwiftUI
import UIKit

/// What a one-time-code form asks for before it can send a code. `.sms` and `.email` are defined
/// next to their views.
struct OTPDestination {
    /// Under the empty field: what the button will do.
    let caption: String
    let prompt: String
    let keyboardType: UIKeyboardType
    let contentType: UITextContentType
    /// Shown under the field once what's typed can't be sent to.
    let invalidHint: String
    let changeLabel: String
    let addressFieldIdentifier: String
    let identifierPrefix: String
    /// The address in the shape the provider needs, or nil when it isn't one yet.
    let normalize: (String) -> String?
    /// How the address reads back in "We sent a code to …".
    let display: (String) -> String
}

/// The code's rules and look, shared by both methods.
enum OTPCode {
    static let length = 6
    /// Wide, so six digits read as a code to copy across rather than as an amount.
    static let tracking: CGFloat = 6
    /// The button and the text actions: what the form keeps above the keyboard.
    static let actionsAnchor = "otp-login-actions"

    /// The provider turned the code down. The one failure that is about what is in the field.
    static var rejectedMessage: String {
        LoginFailureCopy.message(for: .codeRejected, step: .verifyCode)
    }
}

/// The one line under a sign-in field. One slot, so an error, a hint and the explainer never
/// stack up under the field: the most pressing of them is the one that shows.
enum OTPFieldCaption: Equatable {
    /// The last request failed. Set in `loss`.
    case error(String)
    /// What's typed can't be sent to yet.
    case hint(String)
    /// What the button will do, or what just happened.
    case note(String)

    static let sendingNewCode = "Sending a new code\u{2026}"
    static let newCodeSent = "New code sent."

    var text: String {
        switch self {
        case .error(let text), .hint(let text), .note(let text): return text
        }
    }

    var isError: Bool {
        if case .error = self { return true }
        return false
    }

    static func resolve(
        phase: LoginFlow.Phase,
        isCodeStep: Bool,
        explainer: String,
        invalidHint: String?,
        resent: Bool
    ) -> OTPFieldCaption? {
        if case .failed(let message) = phase { return .error(message) }
        if isCodeStep {
            // The read-back above the field already says where the code went.
            if phase == .sendingCode { return .note(sendingNewCode) }
            return resent ? .note(newCodeSent) : nil
        }
        if let invalidHint { return .hint(invalidHint) }
        return .note(explainer)
    }
}

/// The button's words: what it will do, or what it is doing.
enum OTPPrimaryAction {
    static func title(phase: LoginFlow.Phase, isCodeStep: Bool) -> String {
        if isCodeStep {
            // A resend in flight is not the member signing in: the button keeps its name.
            return phase == .verifyingCode ? "Signing you in\u{2026}" : "Continue"
        }
        return phase == .sendingCode ? "Sending code\u{2026}" : "Send code"
    }
}

/// One-time-code sign-in: an address, then the code, then in. SMS and email differ only in the
/// address they ask for, so both run through this one form, and the code rules (6 digits, digits
/// only, auto-submit) live in a single place.
///
/// The code step reads the address back rather than leaving a greyed-out copy of the field on
/// screen, takes the code in the market's mono with wide tracking, and keeps "Send a new code" and
/// "Change number" as text under the button. Whatever needs saying goes in the caption line under
/// the field.
struct OTPLoginForm<Auth: OTPSignIn>: View {
    @ObservedObject var auth: Auth
    let destination: OTPDestination
    /// The login screen's scroll view, so the button can be kept above the keyboard.
    let scroll: ScrollViewProxy
    let send: (String) async -> Void
    let verify: (_ code: String, _ sentTo: String) async -> Void

    @State private var address = ""
    @State private var otpCode: String
    /// A new code went out after the first one. Said once under the field, until the member types.
    @State private var resent = false
    @FocusState private var focusedField: Field?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private enum Field {
        case address
        case code
    }

    init(
        auth: Auth,
        destination: OTPDestination,
        scroll: ScrollViewProxy,
        initialCode: String = "",
        send: @escaping (String) async -> Void,
        verify: @escaping (_ code: String, _ sentTo: String) async -> Void
    ) {
        self.auth = auth
        self.destination = destination
        self.scroll = scroll
        self.send = send
        self.verify = verify
        _otpCode = State(initialValue: initialCode)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if isCodeStep {
                if let sentTo = auth.flow.destination {
                    readBack(sentTo)
                        .padding(.bottom, MonacoTheme.Space.sm)
                }
                codeField
            } else {
                addressField
            }

            if let caption {
                captionLine(caption)
                    .padding(.top, MonacoTheme.Space.s)
            }

            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                primaryButton
                if isCodeStep {
                    codeActions
                }
            }
            .padding(.top, MonacoTheme.Space.l)
            .id(OTPCode.actionsAnchor)
        }
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.2), value: isCodeStep)
        .revealsWhileActive(
            OTPCode.actionsAnchor,
            isActive: focusedField != nil,
            tracking: revealKey,
            proxy: scroll
        )
        .onChange(of: auth.flow.phase) { _, phase in
            guard case .failed(let message) = phase else { return }
            Haptics.warning()
            AccessibilityNotification.Announcement(message).post()
        }
    }

    // MARK: Pieces

    /// "We sent a code to +1 555 123 4567": the sentence in the brand's voice, the address in
    /// the market's, as an address is set everywhere else in the app.
    private func readBack(_ sentTo: String) -> some View {
        let sentAddress: Text = Text(destination.display(sentTo))
            .font(MonacoTheme.Typo.data)
            .foregroundStyle(MonacoTheme.ink)
        return Text("We sent a code to \(sentAddress)")
            .font(MonacoTheme.Typo.callout)
            .foregroundStyle(MonacoTheme.muted)
            .fixedSize(horizontal: false, vertical: true)
    }

    private var addressField: some View {
        TextField(
            destination.prompt,
            text: $address,
            prompt: Text(destination.prompt).foregroundStyle(MonacoTheme.disabledLabel)
        )
        .keyboardType(destination.keyboardType)
        .textContentType(destination.contentType)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .submitLabel(.send)
        .onSubmit {
            Task { await sendCode() }
        }
        .focused($focusedField, equals: .address)
        .authTextFieldStyle(isFocused: focusedField == .address)
        .accessibilityIdentifier(destination.addressFieldIdentifier)
    }

    private var codeField: some View {
        TextField(
            "6-digit code",
            text: $otpCode,
            prompt: Text("6-digit code")
                .font(MonacoTheme.Typo.body)
                .foregroundStyle(MonacoTheme.disabledLabel)
        )
        .font(MonacoTheme.Typo.data)
        // Only once there are digits: tracked out, the placeholder would read as a code too.
        .tracking(otpCode.isEmpty ? 0 : OTPCode.tracking)
        .keyboardType(.numberPad)
        .textContentType(.oneTimeCode)
        .focused($focusedField, equals: .code)
        .authTextFieldStyle(isFocused: focusedField == .code, isInvalid: codeWasRejected)
        .accessibilityIdentifier("\(destination.identifierPrefix)CodeField")
        .onChange(of: otpCode) { _, newValue in
            codeChanged(to: newValue)
        }
    }

    @ViewBuilder
    private func captionLine(_ caption: OTPFieldCaption) -> some View {
        let line = Text(caption.text)
            .font(MonacoTheme.Typo.caption)
            .foregroundStyle(caption.isError ? MonacoTheme.loss : MonacoTheme.muted)
        switch caption {
        case .hint:
            line
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("\(destination.identifierPrefix)AddressHint")
        case .error, .note:
            line
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    @ViewBuilder
    private var primaryButton: some View {
        let title = OTPPrimaryAction.title(phase: auth.flow.phase, isCodeStep: isCodeStep)
        if isCodeStep {
            Button {
                Task { await submitCode(otpCode) }
            } label: {
                Text(title)
            }
            .buttonStyle(.monacoPrimary)
            .disabled(isVerifyDisabled)
            .accessibilityIdentifier("\(destination.identifierPrefix)VerifyButton")
        } else {
            Button {
                Task { await sendCode() }
            } label: {
                Text(title)
            }
            .buttonStyle(.monacoPrimary)
            .disabled(isSendDisabled)
            .accessibilityIdentifier("\(destination.identifierPrefix)SendCodeButton")
        }
    }

    /// Side by side, the way Home sets "Add money" and "Cash out"; one under the other at the
    /// accessibility sizes, where two labels in a row run into each other.
    private var codeActions: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 0))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.l))
        return layout {
            textAction("Send a new code", identifier: "\(destination.identifierPrefix)ResendCodeButton") {
                Task { await resendCode() }
            }
            textAction(destination.changeLabel, identifier: "\(destination.identifierPrefix)ChangeAddressButton") {
                changeAddress()
            }
        }
    }

    private func textAction(_ title: String, identifier: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(MonacoTheme.Typo.calloutStrong)
                .foregroundStyle(auth.flow.isBusy ? MonacoTheme.muted : MonacoTheme.brand)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(auth.flow.isBusy)
        .accessibilityIdentifier(identifier)
    }

    // MARK: Actions

    private func sendCode() async {
        guard let normalized = destination.normalize(address) else { return }
        await send(normalized)
        if case .awaitingCode = auth.flow.phase {
            focusedField = .code
        }
    }

    /// Keeps whatever the member has typed unless the new code actually went out: a throttled
    /// resend used to wipe the field it was about to be typed into.
    private func resendCode() async {
        guard let sentTo = sentDestination else { return }
        await send(sentTo)
        if case .awaitingCode = auth.flow.phase {
            otpCode = ""
            resent = true
            focusedField = .code
        }
    }

    private func changeAddress() {
        otpCode = ""
        resent = false
        auth.resetLoginFlow()
        // Next turn of the run loop: the address field is only back on screen after this update.
        DispatchQueue.main.async {
            focusedField = .address
        }
    }

    private func codeChanged(to newValue: String) {
        let sanitized = String(newValue.filter(\.isASCIIDigit).prefix(OTPCode.length))
        if sanitized != newValue {
            otpCode = sanitized
        }
        if !sanitized.isEmpty {
            resent = false
        }
        // Autofill drops all six digits in at once: don't make them tap Continue too.
        guard sanitized.count == OTPCode.length, !auth.flow.isBusy else { return }
        Task { await submitCode(sanitized) }
    }

    private func submitCode(_ code: String) async {
        guard code.count == OTPCode.length, !auth.flow.isBusy, let sentTo = sentDestination else { return }
        await verify(code, sentTo)
    }

    // MARK: Derived state

    /// The address the code actually went to, not whatever is in the field now.
    private var sentDestination: String? {
        auth.flow.destination ?? destination.normalize(address)
    }

    private var isCodeStep: Bool {
        if case .authenticated = auth.flow.phase { return true }
        return auth.flow.isCodeEntry
    }

    private var showsAddressHint: Bool {
        !isCodeStep
            && !address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && destination.normalize(address) == nil
    }

    private var isSendDisabled: Bool {
        destination.normalize(address) == nil || auth.flow.isBusy
    }

    private var isVerifyDisabled: Bool {
        otpCode.count != OTPCode.length || auth.flow.isBusy
    }

    /// Only a turned-down code marks the field; "no connection" is not about what is in it.
    private var codeWasRejected: Bool {
        auth.flow.phase == .failed(message: OTPCode.rejectedMessage)
    }

    private var caption: OTPFieldCaption? {
        OTPFieldCaption.resolve(
            phase: auth.flow.phase,
            isCodeStep: isCodeStep,
            explainer: destination.caption,
            invalidHint: showsAddressHint ? destination.invalidHint : nil,
            resent: resent
        )
    }

    /// Changes whenever the block under the field changes height, so it is revealed again.
    private var revealKey: String {
        "\(isCodeStep)|\(caption?.text ?? "")"
    }
}

private extension Character {
    /// `isNumber` also matches "½" and non-Latin digits, which no OTP field wants.
    var isASCIIDigit: Bool {
        isASCII && isNumber
    }
}
