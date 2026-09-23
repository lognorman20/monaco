import Foundation

/// Dollar label for a proposal's USDC micros (1 USD = 1,000,000 micros), e.g. "$1,250.00".
public enum ProposalAmountFormatter {
    public static func dollars(fromMicros raw: String) -> String {
        guard let micros = Int64(raw.trimmingCharacters(in: .whitespaces)) else { return raw }
        let value = Decimal(micros) / Decimal(1_000_000)
        return dollarsFormatter.string(from: value as NSDecimalNumber) ?? raw
    }

    /// Built once: every proposal card formats its amount on every body pass.
    private static let dollarsFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        return formatter
    }()
}

/// Share or token count for a sell proposal's `tokenAmount` (atomic units), e.g. "0.5".
public enum ProposalShareFormatter {
    public static let defaultDecimals = AssetCatalogDefaults.decimals

    public static func shares(fromAtomics raw: String, decimals: Int = defaultDecimals) -> String {
        guard let quantity = TokenQuantityFormatter.quantity(fromAtomics: raw, decimals: decimals) else { return raw }
        return sharesFormatter(maxFractionDigits: decimals).string(from: quantity as NSDecimalNumber) ?? raw
    }

    private static func sharesFormatter(maxFractionDigits: Int) -> NumberFormatter {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.numberStyle = .decimal
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = maxFractionDigits
        return formatter
    }
}

/// One dot per eligible voter on a proposal card: yes votes first, then no, then still to vote.
public enum ProposalVoteDot: Equatable, Sendable {
    case yes
    case no
    case pending
}

/// Tally, pass rule, and dots for a proposal card.
public struct ProposalVoteProgress: Equatable {
    /// Above this many voters the card shows the caption only; dots stop reading as a tally.
    public static let maxDots = 12

    public let yesCount: Int
    public let noCount: Int
    public let eligibleCount: Int
    public let threshold: String

    public init(summary: ProposalVoteSummaryDTO) {
        yesCount = max(summary.yesCount, 0)
        noCount = max(summary.noCount, 0)
        // Members can leave after voting; never count more ballots than voters.
        eligibleCount = max(summary.eligibleCount, yesCount + noCount, 0)
        threshold = summary.threshold.lowercased()
    }

    public var votedCount: Int {
        yesCount + noCount
    }

    public var pendingCount: Int {
        max(eligibleCount - votedCount, 0)
    }

    /// Yes votes the proposal needs to pass. Mirrors the backend rule in `packages/domain/votes.go`:
    /// majority passes when yes > no + remaining (floor(n/2) + 1), unanimous needs every voter.
    public var yesNeeded: Int {
        threshold == "unanimous" ? eligibleCount : eligibleCount / 2 + 1
    }

    /// Open proposals, e.g. "2 of 5 voted · 3 yes to pass".
    public var caption: String {
        guard eligibleCount > 0 else { return "No one can vote on this yet" }
        return "\(votedCount) of \(eligibleCount) voted · \(yesNeeded) yes to pass"
    }

    /// Closed proposals, e.g. "3 yes · 1 no".
    public var closedCaption: String {
        "\(yesCount) yes · \(noCount) no"
    }

    /// Nil when there are no voters or more than `maxDots`.
    public var dots: [ProposalVoteDot]? {
        guard eligibleCount > 0, eligibleCount <= Self.maxDots else { return nil }
        return Array(repeating: .yes, count: yesCount)
            + Array(repeating: .no, count: noCount)
            + Array(repeating: .pending, count: pendingCount)
    }
}

/// Where a trade proposal is between the vote and the on-chain swap.
public enum ProposalExecutionStage: Equatable, Sendable {
    case voting
    case executing
    case done
    case failed

    /// Tracker position: Voting (0) → Buying (1) → Done (2). A failed swap stops on step 1.
    public var stepIndex: Int {
        switch self {
        case .voting: 0
        case .executing, .failed: 1
        case .done: 2
        }
    }

    /// Nil when there is nothing to track: agent proposals, votes that failed or expired,
    /// and passed proposals that never had a swap (seeded, read-only).
    public static func of(_ proposal: ProposalDTO) -> ProposalExecutionStage? {
        guard proposal.isTrade else { return nil }
        switch ProposalStatusDisplay.from(status: proposal.status) {
        case .open:
            return .voting
        case .passed:
            switch proposal.execution?.state.lowercased() {
            case "pending": return .executing
            case "confirmed": return .done
            case "failed": return .failed
            default: return nil
            }
        case .failed, .expired, .none:
            return nil
        }
    }
}

extension ProposalDTO {
    /// The detail screen polls while the swap is in flight and stops once it lands or fails.
    public var isAwaitingExecution: Bool {
        ProposalExecutionStage.of(self) == .executing
    }

    /// The viewer's ballot on a detail payload, or nil when they have not voted (or the id is unknown).
    public func viewerChoice(viewerId: String?) -> String? {
        guard let viewerId, !viewerId.isEmpty else { return nil }
        return votes?.first { $0.voterId == viewerId }?.choice.lowercased()
    }
}

public enum ProposalTimeFormatter {
    public static func parse(_ raw: String) -> Date? {
        SharedFormatters.iso8601WholeSeconds.date(from: raw) ?? SharedFormatters.iso8601Fractional.date(from: raw)
    }

    /// Time left on an open vote, e.g. "Closes in 2d", "Closes in 20h", "Closes in 12m".
    /// Nil when the timestamp is unreadable.
    public static func closesLabel(expiresAt raw: String, now: Date = Date()) -> String? {
        guard let expiry = parse(raw) else { return nil }
        let remaining = Int(expiry.timeIntervalSince(now))
        if remaining <= 0 { return "Voting closed" }
        let days = remaining / 86_400
        let hours = remaining / 3600
        let minutes = remaining / 60
        if days >= 2 { return "Closes in \(days)d" }
        if hours > 0 { return "Closes in \(hours)h" }
        return "Closes in \(max(minutes, 1))m"
    }

    /// True in the last hour of an open vote, when the card flags the deadline.
    public static func closesSoon(expiresAt raw: String, now: Date = Date()) -> Bool {
        guard let expiry = parse(raw) else { return false }
        let remaining = expiry.timeIntervalSince(now)
        return remaining > 0 && remaining < 3600
    }

    /// Compact age for comments and cards, e.g. "now", "12m", "3h", "4d", then a short date.
    public static func ageLabel(_ raw: String, now: Date = Date(), calendar: Calendar = .current) -> String {
        guard let date = parse(raw) else { return "" }
        let elapsed = Int(now.timeIntervalSince(date))
        if elapsed < 60 { return "now" }
        if elapsed < 3600 { return "\(elapsed / 60)m" }
        if elapsed < 86_400 { return "\(elapsed / 3600)h" }
        if elapsed < 7 * 86_400 { return "\(elapsed / 86_400)d" }
        return SharedFormatters.string(from: date, pattern: .template("MMMd"), locale: .current, calendar: calendar)
    }
}
