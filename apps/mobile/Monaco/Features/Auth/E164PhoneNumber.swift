import Foundation

/// A phone number in the only shape sign-in accepts: "+", then 8 to 15 digits, nothing else,
/// with a country calling code we can name.
///
/// The field invites iOS autofill, and autofill hands back the member's own number as
/// "+1 (555) 123-4567". Parsing happens here, once, before the round trip, so the form can
/// keep "Send code" disabled instead of spending a request to find out.
///
/// This is the one parser. Dynamic's SMS API takes the number split into a dial code, a
/// region and the subscriber number (`PhoneData`), and that split is read off the same
/// value the form validated — there is no second rule about how many digits are enough.
struct E164PhoneNumber: Equatable {
    /// "+15551234567".
    let value: String
    /// The country calling code, without the "+": "1", "44".
    let countryCode: String
    /// The ISO 3166-1 region Dynamic expects beside the dial code: "US", "GB".
    let regionCode: String

    /// Everything after the country code: "5551234567".
    var nationalNumber: String {
        String(value.dropFirst(1 + countryCode.count))
    }

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
        // interpretation left.
        let international: String
        if leadsWithPlus {
            international = digits
        } else if Self.isNANPNational(digits) {
            international = "1" + digits
        } else if digits.count == 11, digits.hasPrefix("1"), Self.isNANPNational(String(digits.dropFirst())) {
            international = digits
        } else if digits.hasPrefix("00") {
            international = String(digits.dropFirst(2))
        } else {
            // No country code, and not a US-shaped number: we'd only be guessing.
            return nil
        }

        // The single length rule: E.164 allows at most 15 digits, and nothing shorter than
        // 8 reaches a mobile phone anywhere.
        guard (8...15).contains(international.count), !international.hasPrefix("0") else { return nil }
        // A country code we cannot name cannot be sent: Dynamic needs the region, and
        // guessing one would text somebody else's number.
        guard let country = Self.callingCode(prefixing: international) else { return nil }

        value = "+" + international
        countryCode = country.code
        regionCode = country.region
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

    /// Country calling codes are prefix-free, so at most one entry matches a number.
    static func callingCode(prefixing digits: String) -> (code: String, region: String)? {
        callingCodes.first { digits.hasPrefix($0.code) }
    }

    static let callingCodes: [(code: String, region: String)] = [
        ("1", "US"), ("7", "RU"), ("20", "EG"), ("27", "ZA"), ("30", "GR"), ("31", "NL"),
        ("32", "BE"), ("33", "FR"), ("34", "ES"), ("36", "HU"), ("39", "IT"), ("40", "RO"),
        ("41", "CH"), ("43", "AT"), ("44", "GB"), ("45", "DK"), ("46", "SE"), ("47", "NO"),
        ("48", "PL"), ("49", "DE"), ("51", "PE"), ("52", "MX"), ("53", "CU"), ("54", "AR"),
        ("55", "BR"), ("56", "CL"), ("57", "CO"), ("58", "VE"), ("60", "MY"), ("61", "AU"),
        ("62", "ID"), ("63", "PH"), ("64", "NZ"), ("65", "SG"), ("66", "TH"), ("81", "JP"),
        ("82", "KR"), ("84", "VN"), ("86", "CN"), ("90", "TR"), ("91", "IN"), ("92", "PK"),
        ("93", "AF"), ("94", "LK"), ("95", "MM"), ("98", "IR"), ("212", "MA"), ("213", "DZ"),
        ("216", "TN"), ("218", "LY"), ("220", "GM"), ("234", "NG"), ("254", "KE"), ("255", "TZ"),
        ("256", "UG"), ("351", "PT"), ("352", "LU"), ("353", "IE"), ("354", "IS"), ("358", "FI"),
        ("370", "LT"), ("371", "LV"), ("372", "EE"), ("380", "UA"), ("381", "RS"), ("385", "HR"),
        ("386", "SI"), ("420", "CZ"), ("421", "SK"), ("852", "HK"), ("853", "MO"), ("855", "KH"),
        ("856", "LA"), ("880", "BD"), ("886", "TW"), ("960", "MV"), ("961", "LB"), ("962", "JO"),
        ("963", "SY"), ("964", "IQ"), ("965", "KW"), ("966", "SA"), ("971", "AE"), ("972", "IL"),
        ("973", "BH"), ("974", "QA"), ("975", "BT"), ("976", "MN"), ("977", "NP"), ("992", "TJ"),
        ("993", "TM"), ("994", "AZ"), ("995", "GE"), ("996", "KG"), ("998", "UZ"),
    ]
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
