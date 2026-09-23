import Foundation

/// "Activity on AAPLc": what the member's cabals have actually done with this stock.
///
/// This is Monaco's answer to a news feed. There is no news vendor behind the app,
/// and a fabricated headline would be worse than none — but "Weekend investors voted
/// to buy $500" is both true and the thing a member actually wants to know.
public enum AssetActivityCopy {
    /// One rendered line.
    public struct Line: Equatable, Sendable, Identifiable {
        public let id: String
        public let groupId: String
        /// "Weekend investors bought $500 of AAPLc".
        public let title: String
        /// "2h" — the compact age, or "" when the row carries no timestamp.
        public let age: String
        /// SF Symbol name for the leading glyph.
        public let glyph: String
        public let tone: Tone

        public init(id: String, groupId: String, title: String, age: String, glyph: String, tone: Tone) {
            self.id = id
            self.groupId = groupId
            self.title = title
            self.age = age
            self.glyph = glyph
            self.tone = tone
        }

        /// The row spoken as one sentence, so VoiceOver does not read "2h" as a
        /// separate, contextless element.
        public var spoken: String {
            age.isEmpty ? title : "\(title), \(age) ago"
        }
    }

    /// How a line reads: a fill is money that moved, a rejection is not a loss.
    public enum Tone: Equatable, Sendable {
        case neutral
        case positive
        case negative
    }

    public static func lines(_ activity: [AssetActivityDTO], symbol: String, now: Date = Date(), calendar: Calendar = .current) -> [Line] {
        let ticker = AssetSymbolFormatter.format(symbol)
        return activity.map { line($0, ticker: ticker, now: now, calendar: calendar) }
    }

    static func line(_ item: AssetActivityDTO, ticker: String, now: Date, calendar: Calendar) -> Line {
        Line(
            id: item.id,
            groupId: item.groupId,
            title: title(item, ticker: ticker),
            age: item.createdAt.map { age($0, now: now, calendar: calendar) } ?? "",
            glyph: glyph(item.kind),
            tone: tone(item.kind)
        )
    }

    /// The cabal's name leads every line: the member is reading about their friends,
    /// not about a ticker.
    static func title(_ item: AssetActivityDTO, ticker: String) -> String {
        let cabal = item.groupName.isEmpty ? "A cabal" : item.groupName
        let size = sizeClause(item, ticker: ticker)
        switch item.kind {
        case .proposed:
            return "\(cabal) proposed \(verb(item.action)) \(size)"
        case .passed:
            return "\(cabal) voted to \(bareVerb(item.action)) \(size)"
        case .failed:
            return "\(cabal) voted down \(verb(item.action)) \(size)"
        case .expired:
            return "\(cabal)'s vote on \(ticker) expired"
        case .filled:
            return "\(cabal) \(pastVerb(item.action)) \(size)"
        case .unknown:
            return "\(cabal) · \(ticker)"
        }
    }

    /// "$500 of AAPLc" for a buy, "12 AAPLc" for a sell, and the bare ticker when
    /// the row carries no size at all.
    static func sizeClause(_ item: AssetActivityDTO, ticker: String) -> String {
        if item.usdcMicros > 0 {
            return "\(UsdAmountFormatter.format(micros: item.usdcMicros)) of \(ticker)"
        }
        if item.tokenAmount > 0 {
            let units = ProposalShareFormatter.sharesLabel(fromAtomics: String(item.tokenAmount))
            // "1.5 shares" reads oddly next to a token; say the ticker instead.
            let stripped = units
                .replacingOccurrences(of: " shares", with: "")
                .replacingOccurrences(of: " share", with: "")
            return "\(stripped) \(ticker)"
        }
        return ticker
    }

    private static func verb(_ action: AssetProposalKind) -> String {
        switch action {
        case .buy: return "buying"
        case .sell: return "selling"
        case .unknown: return "a trade in"
        }
    }

    private static func bareVerb(_ action: AssetProposalKind) -> String {
        switch action {
        case .buy: return "buy"
        case .sell: return "sell"
        case .unknown: return "trade"
        }
    }

    private static func pastVerb(_ action: AssetProposalKind) -> String {
        switch action {
        case .buy: return "bought"
        case .sell: return "sold"
        case .unknown: return "traded"
        }
    }

    static func glyph(_ kind: AssetActivityKind) -> String {
        switch kind {
        case .proposed: return "hand.raised"
        case .passed: return "checkmark.circle"
        case .failed: return "xmark.circle"
        case .expired: return "clock.badge.xmark"
        case .filled: return "arrow.left.arrow.right.circle.fill"
        case .unknown: return "circle"
        }
    }

    static func tone(_ kind: AssetActivityKind) -> Tone {
        switch kind {
        case .passed, .filled: return .positive
        case .failed: return .negative
        case .proposed, .expired, .unknown: return .neutral
        }
    }

    /// "now", "15m", "3h", then a date — the same ladder the group activity list
    /// already uses, reached from a `Date` rather than from an ISO string.
    static func age(_ date: Date, now: Date, calendar: Calendar) -> String {
        let elapsed = now.timeIntervalSince(date)
        if elapsed < 60 { return "now" }
        if elapsed < 3600 { return "\(Int(elapsed / 60))m" }
        if elapsed < 86_400 { return "\(Int(elapsed / 3600))h" }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return SharedFormatters.string(
            from: date,
            pattern: .fixed(sameYear ? "MMM d" : "MMM d, yyyy"),
            locale: Locale(identifier: "en_US_POSIX"),
            calendar: calendar
        )
    }
}
