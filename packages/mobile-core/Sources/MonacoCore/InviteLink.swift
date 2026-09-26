import Foundation

/// What a member pasted, typed, scanned or opened, resolved to the one thing the join screen
/// needs: a short invite code, or the legacy cabal id that invites used to be.
///
/// Accepted forms:
/// - `monaco://join/K7QM4XPD` (the app's own scheme)
/// - `https://trymonaco.xyz/join/K7QM4XPD`, with or without `www.` or the scheme
/// - the whole share text, "Join Sunday Investors on Monaco: https://trymonaco.xyz/join/K7QM4XPD"
/// - a bare code, any case, grouped with a space or a dash: `k7qm-4xpd`
/// - a cabal id (UUID), which joins through the old route
///
/// Codes are eight symbols of `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`: no 0, O, 1 or I, so
/// "AB12CD34" is not a code. Keep the rules in step with the API (`internal/app/invites.go`)
/// and the landing page (`apps/web/lib/invite.js`).
public enum InviteLink: Equatable, Sendable {
    case code(String)
    case groupId(String)

    public static let alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
    public static let codeLength = 8
    public static let scheme = "monaco"
    public static let webHost = "trymonaco.xyz"

    /// The canonical code, when this is one.
    public var code: String? {
        if case .code(let code) = self { return code }
        return nil
    }

    /// The legacy cabal id, when this is one.
    public var groupId: String? {
        if case .groupId(let id) = self { return id }
        return nil
    }

    // MARK: - Parsing

    public static func parse(_ raw: String) -> InviteLink? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        if let token = token(inLinkWithin: trimmed) {
            return parseToken(token)
        }
        return parseToken(trimmed)
    }

    /// A URL the app was opened with: `monaco://join/<code>` or a universal link to
    /// `https://trymonaco.xyz/join/<code>`. Anything else is nil, so the caller can ignore it.
    public static func parse(url: URL) -> InviteLink? {
        guard let scheme = url.scheme?.lowercased() else { return nil }
        let parts = url.pathComponents.filter { $0 != "/" }
        switch scheme {
        case Self.scheme:
            // monaco://join/<code>: "join" is the host, the code the only path component.
            guard url.host?.lowercased() == "join", parts.count == 1 else { return nil }
            return parseToken(parts[0])
        case "https", "http":
            guard let host = url.host?.lowercased(), host == webHost || host == "www." + webHost,
                  parts.count == 2, parts[0] == "join" else { return nil }
            return parseToken(parts[1])
        default:
            return nil
        }
    }

    /// The text is, or contains, one of the invite links: returns what follows `/join/`.
    private static func token(inLinkWithin text: String) -> String? {
        let range = NSRange(text.startIndex..., in: text)
        guard let match = linkPattern.firstMatch(in: text, range: range),
              let tokenRange = Range(match.range(at: 1), in: text) else { return nil }
        let token = String(text[tokenRange])
        return token.removingPercentEncoding ?? token
    }

    private static let linkPattern = try! NSRegularExpression(
        pattern: #"(?:monaco://join/|(?:https?://|(?<![\w.-]))(?:www\.)?trymonaco\.xyz/join/)([^\s/?#"'<>]+)"#,
        options: [.caseInsensitive]
    )

    private static func parseToken(_ token: String) -> InviteLink? {
        if let code = normalizeCode(token) {
            return .code(code)
        }
        let trimmed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        if UUID(uuidString: trimmed) != nil {
            return .groupId(trimmed.lowercased())
        }
        return nil
    }

    // MARK: - Codes

    /// Uppercases, drops spaces and dashes, and checks the alphabet and the length.
    public static func normalizeCode(_ raw: String) -> String? {
        let squeezed = squeeze(raw)
        guard squeezed.count == codeLength, squeezed.allSatisfy({ alphabet.contains($0) }) else { return nil }
        return squeezed
    }

    /// True while what has been typed could still become a code: only alphabet symbols, and
    /// fewer than eight. The join screen stays quiet for these instead of calling them wrong.
    public static func isPartialCode(_ raw: String) -> Bool {
        let squeezed = squeeze(raw)
        return !squeezed.isEmpty && squeezed.count < codeLength && squeezed.allSatisfy({ alphabet.contains($0) })
    }

    private static func squeeze(_ raw: String) -> String {
        raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .uppercased()
            .filter { $0 != " " && $0 != "-" }
    }

    /// Two groups of four, the way the code is shown: "K7QM 4XPD".
    public static func displayCode(_ code: String) -> String {
        guard code.count == codeLength else { return code }
        let middle = code.index(code.startIndex, offsetBy: codeLength / 2)
        return "\(code[..<middle]) \(code[middle...])"
    }

    // MARK: - Links

    /// The link a member shares, `https://trymonaco.xyz/join/<code>`.
    public static func webURL(for code: String) -> URL {
        URL(string: "https://\(webHost)/join/\(code)")!
    }

    /// The app's own link, `monaco://join/<code>`.
    public static func appURL(for code: String) -> URL {
        URL(string: "\(scheme)://join/\(code)")!
    }
}

/// The words a member shares with the link. Pure so the share sheet and the tests agree.
public enum InviteShareText {
    public static func message(cabalName: String, link: URL) -> String {
        "Join \(cabalName) on Monaco: \(link.absoluteString)"
    }

    public static func subject(cabalName: String) -> String {
        "Join \(cabalName) on Monaco"
    }
}
