import Foundation

/// A phone number in the only shape Privy accepts: "+", then 8 to 15 digits, nothing else.
///
/// The field invites iOS autofill, and autofill hands back the member's own number as
/// "+1 (555) 123-4567". That used to go to Privy verbatim — anything starting with "+" was
/// passed straight through — so the most ordinary way to fill the field ended in a generic
/// "couldn't send the code". Parsing happens here, once, before the round trip.
struct E164PhoneNumber: Equatable {
    /// "+15551234567".
    let value: String

    /// Reads what a member typed, pasted or autofilled. Returns nil when it is not a number
    /// we can send to, so the form can say so instead of spending a request to find out.
    ///
    /// A bare 10-digit number, or 11 starting with 1, is taken as US. Everything else needs
    /// its country code, written either as "+44…" or as "0044…".
    init?(_ input: String) {
        var digits = ""
        var leadsWithPlus = false
        for scalar in input.unicodeScalars {
            if CharacterSet.asciiDigits.contains(scalar) {
                digits.unicodeScalars.append(scalar)
            } else if scalar == "+" {
                // A "+" is a country-code marker only at the front, and only once.
                guard digits.isEmpty, !leadsWithPlus else { return nil }
                leadsWithPlus = true
            } else if CharacterSet.letters.contains(scalar) || CharacterSet.decimalDigits.contains(scalar) {
                // Letters, and digits in other scripts, are not something to guess at.
                return nil
            }
            // Anything else — spaces, brackets, dashes, the direction marks Contacts adds —
            // is formatting.
        }
        guard !digits.isEmpty else { return nil }

        let national: String
        if leadsWithPlus {
            national = digits
        } else if digits.hasPrefix("00") {
            national = String(digits.dropFirst(2))
        } else if digits.count == 10 {
            national = "1" + digits
        } else if digits.count == 11, digits.hasPrefix("1") {
            national = digits
        } else {
            // No country code, and not a US-shaped number: we'd only be guessing.
            return nil
        }

        guard (8...15).contains(national.count), !national.hasPrefix("0") else { return nil }
        value = "+" + national
    }

    /// "(555) 123-4567" for US numbers, otherwise the E.164 form.
    var displayValue: String {
        let digits = value.dropFirst()
        guard digits.hasPrefix("1"), digits.count == 11 else { return value }
        let d = Array(digits.dropFirst())
        return "(\(String(d[0..<3]))) \(String(d[3..<6]))-\(String(d[6..<10]))"
    }
}

private extension CharacterSet {
    static let asciiDigits = CharacterSet(charactersIn: "0123456789")
}
