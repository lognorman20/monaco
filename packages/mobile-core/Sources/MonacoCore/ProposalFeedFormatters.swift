import Foundation

/// Dollar label for a proposal's USDC micros (1 USD = 1,000,000 micros), e.g. "$1,250.00".
public enum ProposalAmountFormatter {
    public static func dollars(fromMicros raw: String) -> String {
        guard let micros = Int64(raw.trimmingCharacters(in: .whitespaces)) else { return raw }
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        let value = Decimal(micros) / Decimal(1_000_000)
        return formatter.string(from: value as NSDecimalNumber) ?? raw
    }
}

/// Share count for a sell proposal's `tokenAmount` (atomic units, 8 decimals), e.g. "0.5".
public enum ProposalShareFormatter {
    public static let decimals = 8

    public static func shares(fromAtomics raw: String) -> String {
        guard let atomics = Decimal(string: raw.trimmingCharacters(in: .whitespaces)), atomics >= 0 else { return raw }
        let shares = atomics / Decimal(sign: .plus, exponent: decimals, significand: 1)
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.numberStyle = .decimal
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = decimals
        return formatter.string(from: shares as NSDecimalNumber) ?? raw
    }
}

/// Vote bar fractions and caption for a proposal card.
public struct ProposalVoteProgress: Equatable {
    public let yesCount: Int
    public let noCount: Int
    public let eligibleCount: Int

    public init(summary: ProposalVoteSummaryDTO) {
        yesCount = max(summary.yesCount, 0)
        noCount = max(summary.noCount, 0)
        // Members can leave after voting; never let the bar exceed 100%.
        eligibleCount = max(summary.eligibleCount, summary.yesCount + summary.noCount, 0)
    }

    public var yesFraction: Double {
        eligibleCount == 0 ? 0 : Double(yesCount) / Double(eligibleCount)
    }

    public var noFraction: Double {
        eligibleCount == 0 ? 0 : Double(noCount) / Double(eligibleCount)
    }

    public var pendingCount: Int {
        max(eligibleCount - yesCount - noCount, 0)
    }

    /// e.g. "2 yes · 1 no · 3 still to vote".
    public var caption: String {
        var parts = ["\(yesCount) yes", "\(noCount) no"]
        if pendingCount > 0 {
            parts.append("\(pendingCount) still to vote")
        }
        return parts.joined(separator: " · ")
    }
}

public enum ProposalTimeFormatter {
    public static func parse(_ raw: String) -> Date? {
        let plain = ISO8601DateFormatter()
        if let date = plain.date(from: raw) { return date }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return fractional.date(from: raw)
    }

    /// Time left on an open vote, e.g. "Closes in 5h 20m". Nil when the timestamp is unreadable.
    public static func closesLabel(expiresAt raw: String, now: Date = Date()) -> String? {
        guard let expiry = parse(raw) else { return nil }
        let remaining = Int(expiry.timeIntervalSince(now))
        if remaining <= 0 { return "Voting closed" }
        let days = remaining / 86_400
        let hours = (remaining % 86_400) / 3600
        let minutes = (remaining % 3600) / 60
        if days > 0 { return "Closes in \(days)d \(hours)h" }
        if hours > 0 { return "Closes in \(hours)h \(minutes)m" }
        return "Closes in \(max(minutes, 1))m"
    }

    /// Compact age for comments and cards, e.g. "now", "12m", "3h", "4d", then a short date.
    public static func ageLabel(_ raw: String, now: Date = Date(), calendar: Calendar = .current) -> String {
        guard let date = parse(raw) else { return "" }
        let elapsed = Int(now.timeIntervalSince(date))
        if elapsed < 60 { return "now" }
        if elapsed < 3600 { return "\(elapsed / 60)m" }
        if elapsed < 86_400 { return "\(elapsed / 3600)h" }
        if elapsed < 7 * 86_400 { return "\(elapsed / 86_400)d" }
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.setLocalizedDateFormatFromTemplate("MMMd")
        return formatter.string(from: date)
    }
}
