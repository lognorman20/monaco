import Foundation

/// User-facing copy for Tessera pre-IPO surfaces (Stocks tab, detail, propose nudge).
public enum PreIpoCopy {
    public static let sectionTitle = "Pre-IPO"
    public static let chipLabel = "Pre-IPO"
    public static let tradesAroundTheClock = "Trades around the clock"
    public static let privateMarketReference = "Private-market reference"
    public static let referenceUnavailable = "Reference unavailable"
    public static let companyValueCaption = "Company value"
    public static let aboutCardTitle = "About pre-IPO tokens"
    public static let disclosure =
        "Pre-IPO tokens track a private company before it lists. They pay out if the company goes public or is sold. You can sell anytime here. A 0.2% fee applies when buying and when selling."
    public static let termsLinkTitle = "Terms at tessera.pe"
    public static let termsURL = "https://terms.tessera.pe"
    public static let alsoAvailableFrom = "Also available from"
    public static let chartEmpty = "Price history builds up over time."
    public static let chartEmptyMessage = "The curve draws as the token trades."

    public static let tokenLabelPlural = "tokens"
    public static let tokenLabelSingular = "token"
    public static let tokensRowLabel = "Tokens"

    /// The line under the reference price: the company's value and how old the mark is.
    /// `age` is `RelativeTimeFormatter`'s word: "now", "15m", "3h", or a date. Each reads
    /// differently in a sentence, and "updated now ago" is what gluing them produced.
    public static func referenceCaption(companyValue: String, age: String?) -> String {
        let lead = "\(companyValueCaption) \(companyValue)"
        guard let age = age?.trimmingCharacters(in: .whitespaces), !age.isEmpty else { return lead }
        if age == "now" { return "\(lead) · updated just now" }
        let isElapsed = age.last.map { "mhd".contains($0) } == true && age.dropLast().allSatisfy(\.isNumber)
        return isElapsed ? "\(lead) · updated \(age) ago" : "\(lead) · updated \(age)"
    }

    /// Short chip beside the reference row, e.g. "27% below".
    public static func premiumChip(bps: Int) -> String {
        let pct = abs(bps) / 100
        return bps < 0 ? "\(pct)% below" : "\(pct)% above"
    }

    /// Nudge on propose amount and proposal cards when premium is wide.
    public static func tradingPremiumNudge(bps: Int) -> String {
        let pct = abs(bps) / 100
        let direction = bps < 0 ? "below" : "above"
        return "Trading \(pct)% \(direction) its private-market reference. That gap can widen or close."
    }

    public static func showsPremiumNudge(premiumBps: Int?, assetKind: AssetKind) -> Bool {
        guard let premiumBps else { return false }
        let magnitude = abs(premiumBps)
        switch assetKind {
        case .preIpo: return magnitude >= 1000
        case .stock: return magnitude >= 100
        }
    }

    public static let auditedStrings: [String] = [
        sectionTitle,
        chipLabel,
        tradesAroundTheClock,
        privateMarketReference,
        referenceUnavailable,
        premiumChip(bps: -2700),
        premiumChip(bps: 2700),
        tradingPremiumNudge(bps: -2700),
        tradingPremiumNudge(bps: 2700),
        companyValueCaption,
        aboutCardTitle,
        disclosure,
        termsLinkTitle,
        alsoAvailableFrom,
        chartEmpty,
        chartEmptyMessage,
        referenceCaption(companyValue: "$32.1B", age: "now"),
        tokenLabelPlural,
        tokenLabelSingular,
        tokensRowLabel,
    ]
}
