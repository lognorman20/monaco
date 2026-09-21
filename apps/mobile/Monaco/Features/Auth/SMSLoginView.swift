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
                invalidHint: "Enter a mobile number with its country code, like +1 555 123 4567.",
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
}

#Preview {
    SMSLoginView(auth: DynamicAuthService())
}
