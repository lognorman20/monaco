import SwiftUI

/// Email OTP sign-in via Privy — kept for internal testers.
struct EmailLoginView: View {
    @ObservedObject var auth: PrivyAuthService

    var body: some View {
        OTPLoginForm(
            auth: auth,
            destination: OTPLoginForm.Destination(
                caption: "We’ll email you a one-time code. Check spam if it doesn’t arrive.",
                prompt: "Email address",
                keyboardType: .emailAddress,
                contentType: .emailAddress,
                invalidHint: "Enter an email address, like you@example.com.",
                changeLabel: "Change email",
                addressFieldIdentifier: "emailAddressField",
                identifierPrefix: "email",
                normalize: Self.normalizedEmail,
                display: { $0 }
            ),
            send: { await auth.sendEmailCode(to: $0) },
            verify: { code, email in
                await auth.loginWithEmailCode(code, sentTo: email)
            }
        )
    }

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
    EmailLoginView(auth: PrivyAuthService())
}
