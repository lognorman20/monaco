import SwiftUI

/// Email one-time-code sign-in via Privy: kept for internal testers.
struct EmailLoginView: View {
    @ObservedObject var auth: PrivyAuthService
    let scroll: ScrollViewProxy
    /// Debug harness only: the code the form opens with.
    var initialCode = ""

    var body: some View {
        OTPLoginForm(
            auth: auth,
            destination: .email,
            scroll: scroll,
            initialCode: initialCode
        )
    }
}

extension OTPDestination {
    static let email = OTPDestination(
        channel: .email,
        caption: "We'll email you a one-time code. Check spam if it doesn't arrive.",
        prompt: "Email address",
        keyboardType: .emailAddress,
        contentType: .emailAddress,
        invalidHint: "Enter an email address, like you@example.com.",
        changeLabel: "Change email",
        addressFieldIdentifier: "emailAddressField",
        identifierPrefix: "email",
        normalize: OTPDestination.normalizedEmail,
        display: { $0 }
    )

    private static func normalizedEmail(_ input: String) -> String? {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        // Deliberately loose: the provider is the real judge of an address.
        let parts = trimmed.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty, parts[1].contains("."), !parts[1].hasSuffix(".") else {
            return nil
        }
        return trimmed
    }
}

#Preview {
    ScrollViewReader { proxy in
        EmailLoginView(auth: PrivyAuthService(), scroll: proxy)
            .padding()
    }
}
