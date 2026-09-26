import Foundation

/// How the member signs in: the phone number or email Privy sent their code to.
public enum SignInIdentity: Equatable, Sendable {
    case phone(String)
    case email(String)

    /// The row's label.
    public var label: String {
        switch self {
        case .phone: return "Phone"
        case .email: return "Email"
        }
    }

    /// Enough to recognise, not enough to read off a shoulder: "+1 ••• ••• 7177",
    /// "l•••@monacolabs.xyz".
    public var masked: String {
        switch self {
        case .phone(let number): return Self.maskPhone(number)
        case .email(let address): return Self.maskEmail(address)
        }
    }

    /// The country code and the last four digits, the middle as two blocks of dots whatever
    /// its real length, so the mask gives nothing away about the number's shape.
    static func maskPhone(_ raw: String) -> String {
        let digits = raw.filter(\.isASCIIDigit)
        guard digits.count >= 8 else { return "••• ••• ••••" }
        let last4 = String(digits.suffix(4))
        guard raw.trimmingCharacters(in: .whitespaces).hasPrefix("+") else {
            return "••• ••• \(last4)"
        }
        let codeLength = min(countryCodeLength(digits), digits.count - 4)
        return "+\(digits.prefix(codeLength)) ••• ••• \(last4)"
    }

    /// The first character of the name and the whole domain.
    static func maskEmail(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let at = trimmed.lastIndex(of: "@"), at != trimmed.startIndex else { return "•••" }
        let domain = trimmed[trimmed.index(after: at)...]
        guard !domain.isEmpty, let first = trimmed.first else { return "•••" }
        return "\(first)•••@\(domain)"
    }

    /// ITU E.164 country calling codes are one to three digits and prefix-free: 1 and 7 stand
    /// alone, these two-digit codes are assigned, and every other code is three digits.
    static func countryCodeLength(_ digits: String) -> Int {
        guard let first = digits.first else { return 0 }
        if first == "1" || first == "7" { return 1 }
        let twoDigit: Set<String> = [
            "20", "27", "30", "31", "32", "33", "34", "36", "39", "40", "41", "43", "44", "45",
            "46", "47", "48", "49", "51", "52", "53", "54", "55", "56", "57", "58", "60", "61",
            "62", "63", "64", "65", "66", "81", "82", "84", "86", "90", "91", "92", "93", "94",
            "95", "98",
        ]
        return twoDigit.contains(String(digits.prefix(2))) ? 2 : 3
    }
}

private extension Character {
    var isASCIIDigit: Bool { ("0"..."9").contains(self) }
}

/// How long Monaco may sit in the background before it asks for Face ID again.
public enum AppLockTimeout: String, CaseIterable, Sendable, Identifiable {
    case immediately
    case oneMinute
    case fiveMinutes
    case fifteenMinutes

    public var id: String { rawValue }

    public var seconds: TimeInterval {
        switch self {
        case .immediately: return 0
        case .oneMinute: return 60
        case .fiveMinutes: return 300
        case .fifteenMinutes: return 900
        }
    }

    public var title: String {
        switch self {
        case .immediately: return "Immediately"
        case .oneMinute: return "After 1 minute"
        case .fiveMinutes: return "After 5 minutes"
        case .fifteenMinutes: return "After 15 minutes"
        }
    }

    /// The trailing value on the "Lock after" row.
    public var shortTitle: String {
        switch self {
        case .immediately: return "Immediately"
        case .oneMinute: return "1 minute"
        case .fiveMinutes: return "5 minutes"
        case .fifteenMinutes: return "15 minutes"
        }
    }

    /// Whether coming back at `now` from a background entered at `backgroundedAt` needs the
    /// lock. A clock that went backwards (the member changed the time) locks: when in doubt,
    /// ask.
    public func requiresUnlock(backgroundedAt: Date, now: Date) -> Bool {
        let away = now.timeIntervalSince(backgroundedAt)
        return away < 0 || away >= seconds
    }
}

/// Light, dark, or whatever iOS is set to.
public enum AppearanceChoice: String, CaseIterable, Sendable, Identifiable {
    case system
    case light
    case dark

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .system: return "System"
        case .light: return "Light"
        case .dark: return "Dark"
        }
    }
}

/// The typed confirmation on the delete screen.
public enum DeleteConfirmation {
    public static let word = "DELETE"

    /// The word exactly, give or take surrounding spaces. Case counts: typing it in capitals is
    /// the point of asking.
    public static func matches(_ typed: String) -> Bool {
        typed.trimmingCharacters(in: .whitespacesAndNewlines) == word
    }
}

/// Where Settings sends the member outside the app.
public enum SettingsLinks {
    public static let terms = URL(string: "https://monacolabs.xyz/terms")!
    public static let privacy = URL(string: "https://monacolabs.xyz/privacy")!
    public static let supportAddress = "support@monacolabs.xyz"

    /// A `mailto:` with the version and the member's id already in the body, so support can
    /// find the account without asking for it.
    public static func supportEmail(version: String, build: String, userId: String?) -> URL {
        var components = URLComponents()
        components.scheme = "mailto"
        components.path = supportAddress
        let account = userId.map { "Account: \($0)" } ?? "Account: not signed in"
        components.queryItems = [
            URLQueryItem(name: "subject", value: "Monaco help"),
            URLQueryItem(name: "body", value: "\n\n\nMonaco \(version) (\(build))\n\(account)"),
        ]
        return components.url!
    }

    /// "1.4.0 (212)".
    public static func versionLabel(version: String?, build: String?) -> String {
        let version = version?.trimmingCharacters(in: .whitespaces).nonEmpty ?? "Unknown"
        guard let build = build?.trimmingCharacters(in: .whitespaces).nonEmpty else { return version }
        return "\(version) (\(build))"
    }
}

private extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
