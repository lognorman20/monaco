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
            } else if !CharacterSet.phoneFormatting.contains(scalar) {
                // Not a digit, not the country-code marker, and not formatting we know:
                // letters, digits in other scripts, and characters like "½" or "①" that
                // are numeric to a reader but not digits. Treating those as formatting
                // drops them silently and sends a different number than the one on screen.
                return nil
            }
        }
        guard !digits.isEmpty else { return nil }

        // The branches are mutually exclusive rather than ordered: a NANP area code never
        // starts with 0, so a 10-digit string beginning "00" is not a US number however it
        // is written, and reading it as a trunk-prefixed international one is the only
        // interpretation left. Previously the "00" branch simply ran first, which decided
        // the same question by accident rather than on what the digits mean.
        let national: String
        if leadsWithPlus {
            national = digits
        } else if Self.isNANPNational(digits) {
            national = "1" + digits
        } else if digits.count == 11, digits.hasPrefix("1"), Self.isNANPNational(String(digits.dropFirst())) {
            national = digits
        } else if digits.hasPrefix("00") {
            national = String(digits.dropFirst(2))
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

private extension E164PhoneNumber {
    /// A 10-digit North American number. The area code never starts with 0 or 1, which is
    /// what tells a US number apart from a trunk-prefixed international one. Only the area
    /// code is checked: the exchange code has its own rules, but numbers people actually
    /// type — 555-123-4567 among them — do not all honour them, and this is a routing
    /// question, not a validity one.
    static func isNANPNational(_ digits: String) -> Bool {
        guard digits.count == 10, let first = digits.first else { return false }
        return ("2"..."9").contains(first)
    }
}

private extension CharacterSet {
    static let asciiDigits = CharacterSet(charactersIn: "0123456789")

    /// What legitimately sits between the digits of a written phone number: separators
    /// people type, the spaces Contacts and autofill insert, and the bidi controls that
    /// come with a right-to-left locale. An allowlist rather than a denylist, so a
    /// character we have not accounted for is refused instead of quietly dropped.
    static let phoneFormatting = CharacterSet(charactersIn: "()-./ \t")
        .union(CharacterSet(charactersIn: "\u{00A0}\u{2009}\u{202F}"))
        .union(CharacterSet(charactersIn: "\u{2013}\u{2014}"))
        .union(CharacterSet(charactersIn: "\u{200E}\u{200F}\u{061C}"))
        .union(CharacterSet(charactersIn: "\u{202A}\u{202B}\u{202C}\u{202D}\u{202E}"))
        .union(CharacterSet(charactersIn: "\u{2066}\u{2067}\u{2068}\u{2069}"))
}
