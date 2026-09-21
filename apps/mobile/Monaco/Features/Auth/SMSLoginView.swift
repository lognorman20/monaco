import SwiftUI

/// SMS OTP sign-in — primary demo path.
struct SMSLoginView: View {
    @ObservedObject var auth: DynamicAuthService

    var body: some View {
        OTPLoginForm(
            auth: auth,
            destination: OTPLoginForm.Destination(
                caption: "We’ll text you a code to sign in.",
                prompt: "Phone number",
                keyboardType: .phonePad,
                contentType: .telephoneNumber,
                invalidHint: Self.invalidHint(for:),
                changeLabel: "Change number",
                addressFieldIdentifier: "smsPhoneField",
                identifierPrefix: "sms",
                normalize: { E164PhoneNumber($0)?.value },
                display: { E164PhoneNumber($0)?.displayValue ?? $0 }
            ),
            send: { await auth.sendSMSCode(to: $0) },
            verify: { code, phoneNumber in
                await auth.loginWithSMSCode(code, sentTo: phoneNumber)
            }
        )
    }

    /// A member with a valid number from a country we cannot text yet is told that, rather
    /// than being asked for the country code they already entered.
    static func invalidHint(for input: String) -> String {
        if E164PhoneNumber.isUnsupportedCountry(input) {
            return "We can’t text numbers in that country yet."
        }
        return "Enter a mobile number with its country code, like +1 555 123 4567."
    }
}

#Preview {
    SMSLoginView(auth: DynamicAuthService())
}
