import Foundation

/// The history screen's filter chips, and the `type` the API takes for each.
public enum HistoryFilter: String, CaseIterable, Sendable, Identifiable {
    case all
    case moneyIn = "money_in"
    case cashOut = "cash_out"
    case buy
    case sell

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .all: return "All"
        case .moneyIn: return "Money in"
        case .cashOut: return "Cash outs"
        case .buy: return "Buys"
        case .sell: return "Sells"
        }
    }

    /// What an empty list says under this filter.
    public var emptyTitle: String {
        switch self {
        case .all: return "No money has moved yet"
        case .moneyIn: return "No money in yet"
        case .cashOut: return "No cash outs yet"
        case .buy: return "No buys yet"
        case .sell: return "No sells yet"
        }
    }
}

/// What a history row is, in the member's terms.
public enum HistoryKind: String, Sendable {
    /// USDC that arrived in the account from outside.
    case deposit
    /// Money moved from the account into a cabal.
    case fund
    /// Money taken out of a cabal back to the account.
    case cashOut = "cash_out"
    /// Money sent from the account to an outside address.
    case withdrawal
    case buy
    case sell
    case botBuy = "bot_buy"
    case botSell = "bot_sell"
    case unknown

    public init(raw: String) {
        self = HistoryKind(rawValue: raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()) ?? .unknown
    }

    public var isTrade: Bool {
        switch self {
        case .buy, .sell, .botBuy, .botSell: return true
        default: return false
        }
    }

    public var isMoneyIn: Bool { self == .deposit || self == .fund }
}

public enum HistoryStatus: String, Sendable {
    case pending, done, failed

    public init(raw: String) {
        self = HistoryStatus(rawValue: raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()) ?? .pending
    }
}

/// One row of GET /v1/me/transactions.
public struct HistoryItemDTO: Codable, Equatable, Sendable, Identifiable {
    public let id: String
    public let kind: String
    public let status: String
    public let groupId: String?
    public let groupName: String?
    public let symbol: String?
    public let name: String?
    public let assetKind: AssetKind?
    /// The member's dollars. Nil on a sell that has not filled.
    public let amountUsd: String?
    /// The member's slice of a trade's shares or tokens. Nil on money rows.
    public let quantity: String?
    public let at: Date
    /// The swap behind a buy or sell, for its receipt.
    public let transactionId: String?

    public var resolvedKind: HistoryKind { HistoryKind(raw: kind) }
    public var resolvedStatus: HistoryStatus { HistoryStatus(raw: status) }
    public var resolvedAssetKind: AssetKind { assetKind ?? .stock }

    public init(
        id: String,
        kind: String,
        status: String,
        groupId: String? = nil,
        groupName: String? = nil,
        symbol: String? = nil,
        name: String? = nil,
        assetKind: AssetKind? = nil,
        amountUsd: String?,
        quantity: String? = nil,
        at: Date,
        transactionId: String? = nil
    ) {
        self.id = id
        self.kind = kind
        self.status = status
        self.groupId = groupId
        self.groupName = groupName
        self.symbol = symbol
        self.name = name
        self.assetKind = assetKind
        self.amountUsd = amountUsd
        self.quantity = quantity
        self.at = at
        self.transactionId = transactionId
    }
}

/// One page of history. `nextCursor` is nil on the last page.
public struct HistoryPageDTO: Codable, Equatable, Sendable {
    public let items: [HistoryItemDTO]
    public let nextCursor: String?

    public init(items: [HistoryItemDTO], nextCursor: String?) {
        self.items = items
        self.nextCursor = nextCursor
    }

    private enum CodingKeys: String, CodingKey { case items, nextCursor }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = try container.decodeIfPresent([HistoryItemDTO].self, forKey: .items) ?? []
        let cursor = try container.decodeIfPresent(String.self, forKey: .nextCursor)?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        nextCursor = (cursor?.isEmpty ?? true) ? nil : cursor
    }
}

/// Where tapping a history row goes, when there is a receipt to open.
public enum HistoryReceipt: Equatable, Sendable {
    /// A buy or sell: `GET /v1/transactions/{id}`.
    case transaction(String)
    /// Money moved into a cabal: `GET /v1/deposits/{id}`.
    case deposit(String)
}

/// How a history amount reads.
public enum HistoryAmountTone: Equatable, Sendable {
    /// Money that came in and landed: green, with a plus.
    case moneyIn
    /// Everything else: ink, no sign.
    case plain
    /// Money that never moved: muted.
    case failed
}

/// The words on a history row. Kept here, out of the view, so every title is tested and
/// audited against the product's vocabulary.
public enum HistoryRowCopy {
    /// "Bought Apple", "Added money to Sunday Investors", "Cashing out of Semis or bust".
    public static func title(for item: HistoryItemDTO) -> String {
        let status = item.resolvedStatus
        let cabal = cabalName(item)
        let stock = stockName(item)
        switch item.resolvedKind {
        case .fund:
            return pick(status, done: "Added money to \(cabal)", pending: "Adding money to \(cabal)", failed: "Couldn't add money to \(cabal)")
        case .deposit:
            return pick(status, done: "Added money", pending: "Adding money", failed: "Couldn't add money")
        case .cashOut:
            return pick(status, done: "Cashed out of \(cabal)", pending: "Cashing out of \(cabal)", failed: "Couldn't cash out of \(cabal)")
        case .withdrawal:
            return pick(status, done: "Cashed out", pending: "Cashing out", failed: "Couldn't cash out")
        case .buy:
            return pick(status, done: "Bought \(stock)", pending: "Buying \(stock)", failed: "Couldn't buy \(stock)")
        case .sell:
            return pick(status, done: "Sold \(stock)", pending: "Selling \(stock)", failed: "Couldn't sell \(stock)")
        case .botBuy:
            return pick(status, done: "Agent bought \(stock)", pending: "Agent buying \(stock)", failed: "Agent couldn't buy \(stock)")
        case .botSell:
            return pick(status, done: "Agent sold \(stock)", pending: "Agent selling \(stock)", failed: "Agent couldn't sell \(stock)")
        case .unknown:
            return "Money moved"
        }
    }

    /// The muted line under the title: where the money was, never a repeat of the title.
    public static func caption(for item: HistoryItemDTO) -> String {
        switch item.resolvedKind {
        case .fund, .withdrawal: return "From your account"
        case .deposit, .cashOut: return "To your account"
        case .buy, .sell, .botBuy, .botSell, .unknown: return cabalName(item)
        }
    }

    /// "Pending" or "Failed" beside the caption; nil once the money has moved.
    public static func statusLabel(for item: HistoryItemDTO) -> String? {
        switch item.resolvedStatus {
        case .done: return nil
        case .pending: return "Pending"
        case .failed: return "Failed"
        }
    }

    public static func amountTone(for item: HistoryItemDTO) -> HistoryAmountTone {
        switch item.resolvedStatus {
        case .failed: return .failed
        case .pending: return .plain
        case .done: return item.resolvedKind.isMoneyIn ? .moneyIn : .plain
        }
    }

    /// "+$600.00" for money in that landed, "$480.00" otherwise; nil when the ledger has no
    /// dollar figure yet (a sell that has not filled).
    public static func amountText(for item: HistoryItemDTO) -> String? {
        guard let raw = item.amountUsd?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty,
              Decimal(string: raw, locale: PortfolioMath.posix) != nil
        else { return nil }
        let formatted = UsdAmountFormatter.format(decimalString: raw)
        return amountTone(for: item) == .moneyIn ? "+" + formatted : formatted
    }

    /// "2.4 shares" on a trade that has filled; nil otherwise.
    public static func quantityText(for item: HistoryItemDTO) -> String? {
        guard item.resolvedKind.isTrade,
              let raw = item.quantity?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty
        else { return nil }
        return PortfolioMath.quantityLabel(raw, kind: item.resolvedAssetKind)
    }

    public static func receipt(for item: HistoryItemDTO) -> HistoryReceipt? {
        switch item.resolvedKind {
        case .buy, .sell, .botBuy, .botSell:
            guard let id = item.transactionId?.trimmingCharacters(in: .whitespacesAndNewlines), !id.isEmpty else { return nil }
            return .transaction(id)
        case .fund:
            return .deposit(item.id)
        case .deposit, .cashOut, .withdrawal, .unknown:
            return nil
        }
    }

    /// "Apple" for AAPLx, "SpaceX" for tSpaceX; the ticker when the name is unknown.
    public static func stockName(_ item: HistoryItemDTO) -> String {
        let symbol = item.symbol?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let name = item.name?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !symbol.isEmpty || !name.isEmpty else { return "a stock" }
        let formatted = AssetCatalogDisplayName.format(catalogName: name, symbol: symbol, kind: item.resolvedAssetKind)
        return formatted.isEmpty ? "a stock" : formatted
    }

    static func cabalName(_ item: HistoryItemDTO) -> String {
        let name = item.groupName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? "a cabal" : name
    }

    private static func pick(_ status: HistoryStatus, done: String, pending: String, failed: String) -> String {
        switch status {
        case .done: return done
        case .pending: return pending
        case .failed: return failed
        }
    }

    /// Every fixed string a history row can show, for the copy audit.
    public static let auditedStrings: [String] = HistoryFilter.allCases.flatMap { [$0.title, $0.emptyTitle] } + [
        "Added money to", "Adding money to", "Couldn't add money to", "Added money", "Adding money",
        "Couldn't add money", "Cashed out of", "Cashing out of", "Couldn't cash out of", "Cashed out",
        "Cashing out", "Couldn't cash out", "Bought", "Buying", "Couldn't buy", "Sold", "Selling",
        "Couldn't sell", "Agent bought", "Agent buying", "Agent couldn't buy", "Agent sold",
        "Agent selling", "Agent couldn't sell", "Money moved", "From your account", "To your account",
        "Pending", "Failed", "a cabal", "a stock",
    ]
}

/// A day of history under its heading.
public struct HistoryDaySection: Equatable, Sendable, Identifiable {
    /// The day's start in the calendar used, as a stable id across pages.
    public let id: Date
    public let title: String
    public let items: [HistoryItemDTO]
}

public enum HistoryDayGrouping {
    /// Rows under "Today", "Yesterday", then "Sep 22" (and "Sep 22, 2025" in another year),
    /// in the order they came. The API sends newest first, so a day is contiguous even
    /// when it spans two pages.
    public static func sections(
        _ items: [HistoryItemDTO],
        now: Date = Date(),
        calendar: Calendar = .current
    ) -> [HistoryDaySection] {
        var sections: [HistoryDaySection] = []
        var currentDay: Date?
        var bucket: [HistoryItemDTO] = []
        func flush() {
            guard let day = currentDay, !bucket.isEmpty else { return }
            sections.append(HistoryDaySection(id: day, title: title(for: day, now: now, calendar: calendar), items: bucket))
        }
        for item in items {
            let day = calendar.startOfDay(for: item.at)
            if day != currentDay {
                flush()
                currentDay = day
                bucket = []
            }
            bucket.append(item)
        }
        flush()
        return sections
    }

    public static func title(for day: Date, now: Date = Date(), calendar: Calendar = .current) -> String {
        if calendar.isDate(day, inSameDayAs: now) { return "Today" }
        if let yesterday = calendar.date(byAdding: .day, value: -1, to: now), calendar.isDate(day, inSameDayAs: yesterday) {
            return "Yesterday"
        }
        let sameYear = calendar.component(.year, from: day) == calendar.component(.year, from: now)
        return SharedFormatters.string(
            from: day,
            pattern: .fixed(sameYear ? "MMM d" : "MMM d, yyyy"),
            locale: Locale(identifier: "en_US_POSIX"),
            calendar: calendar
        )
    }

    /// "3:42 PM" for a money row's time, in the member's own clock format.
    public static func timeLabel(_ date: Date, calendar: Calendar = .current, locale: Locale = .current) -> String {
        SharedFormatters.string(from: date, pattern: .template("jmm"), locale: locale, calendar: calendar)
    }
}

/// Screen copy for the portfolio and history screens.
public enum PortfolioCopy {
    public static let title = "Portfolio"
    public static let totalCaption = "Your money in cabals"
    public static let allTime = "all time"
    public static let holdings = "Holdings"
    public static let cash = "Cash"
    public static let cashInCabals = "Cash in cabals"
    public static let cashInCabalsCaption = "Not in a stock yet"
    public static let accountBalance = "Account balance"
    public static let accountBalanceCaption = "Ready to add to a cabal"
    public static let accountBalanceUnavailable = "Couldn't read your balance just now"
    public static let emptyTitle = "Fund a cabal to see your portfolio"
    public static let emptyMessage = "Add money, then move it into a cabal. What the cabal buys shows up here."
    public static let loadFailed = "Couldn't load your portfolio"
    public static let tryAgain = "Try again"
    public static let seePortfolio = "See portfolio"
    public static let history = "History"
    public static let historyTitle = "History"
    public static let historyCaption = "Every dollar in and out, with a CSV export"
    public static let exportCSV = "Export CSV"
    public static let exportFailed = "Couldn't export your history"
    public static let historyLoadFailed = "Couldn't load your history"
    public static let loadMoreFailed = "Couldn't load more"
    public static let emptyHistoryMessage = "Add money and fund a cabal. Every dollar that moves shows up here."

    /// "1 cabal couldn't be valued just now" when the server left some out.
    public static func unvalued(_ count: Int) -> String? {
        guard count > 0 else { return nil }
        return count == 1 ? "1 cabal couldn't be valued just now" : "\(count) cabals couldn't be valued just now"
    }

    /// Where a holding sits: the cabal's name when it is one, "2 cabals" when it is several
    /// (the rows under it name them).
    public static func cabalsSummary(_ names: [String]) -> String {
        guard let first = names.first else { return "" }
        return names.count == 1 ? first : "\(names.count) cabals"
    }

    public static let auditedStrings: [String] = [
        title, totalCaption, allTime, holdings, cash, cashInCabals, cashInCabalsCaption, accountBalance,
        accountBalanceCaption, accountBalanceUnavailable, emptyTitle, emptyMessage, loadFailed, tryAgain,
        seePortfolio, history, historyTitle, historyCaption, exportCSV, exportFailed, historyLoadFailed, loadMoreFailed,
        emptyHistoryMessage, unvalued(1) ?? "", unvalued(2) ?? "", cabalsSummary(["Sunday Investors", "Semis"]),
    ] + HistoryRowCopy.auditedStrings
}
