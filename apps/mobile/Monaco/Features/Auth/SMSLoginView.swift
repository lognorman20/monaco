import SwiftUI

/// SMS one-time-code sign-in via Privy: the main way in.
struct SMSLoginView: View {
    @ObservedObject var auth: PrivyAuthService
    let scroll: ScrollViewProxy
    /// Debug harness only: the code the form opens with.
    var initialCode = ""

    var body: some View {
        OTPLoginForm(
            auth: auth,
            destination: .sms,
            scroll: scroll,
            initialCode: initialCode
        )
    }
}

extension OTPDestination {
    static let sms = OTPDestination(
        channel: .sms,
        caption: "We'll text you a code to sign in.",
        prompt: "Phone number",
        keyboardType: .phonePad,
        contentType: .telephoneNumber,
        invalidHint: "Enter a mobile number with its country code, like +1 555 123 4567.",
        changeLabel: "Change number",
        addressFieldIdentifier: "smsPhoneField",
        identifierPrefix: "sms",
        normalize: { E164PhoneNumber($0)?.value },
        display: PhoneReadBack.format
    )
}

/// How a number reads back once a code has gone to it.
///
/// A North American number reads "+1 555 123 4567": the shape the field's own hint asks for, with
/// the country code that says it went to the right country. Any other number reads in its E.164
/// form, because where its spaces go depends on a country this app does not look up.
enum PhoneReadBack {
    static func format(_ e164: String) -> String {
        let digits = e164.dropFirst()
        guard e164.hasPrefix("+1"),
              digits.count == 11,
              digits.allSatisfy({ $0.isASCII && $0.isNumber })
        else { return e164 }
        let national = Array(digits.dropFirst())
        return "+1 \(String(national[0..<3])) \(String(national[3..<6])) \(String(national[6..<10]))"
    }
}

#Preview {
    ScrollViewReader { proxy in
        SMSLoginView(auth: PrivyAuthService(), scroll: proxy)
            .padding()
    }
}
