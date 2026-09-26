import Foundation

/// One cabal's part of a holding: the member's slice of that cabal's position.
public struct PortfolioCabalLineDTO: Codable, Equatable, Sendable, Identifiable {
    public let groupId: String
    public let name: String
    /// The cabal's tint name (`pine`, `ochre`, `plum`, `indigo`, `moss`). The app derives the
    /// same tint from `groupId`; the field is here for clients that only have this payload.
    public let tint: String?
    public let pictureUrl: String?
    public let valueUsd: String
    /// The member's slice of the cabal's shares or tokens, as a decimal string.
    public let quantity: String
    public let dollarPnl: String

    public var id: String { groupId }

    public init(
        groupId: String,
        name: String,
        tint: String? = nil,
        pictureUrl: String? = nil,
        valueUsd: String,
        quantity: String,
        dollarPnl: String
    ) {
        self.groupId = groupId
        self.name = name
        self.tint = tint
        self.pictureUrl = pictureUrl
        self.valueUsd = valueUsd
        self.quantity = quantity
        self.dollarPnl = dollarPnl
    }
}

/// One stock across every cabal the member holds a slice of.
public struct PortfolioHoldingDTO: Codable, Equatable, Sendable, Identifiable {
    public let symbol: String
    /// The catalogue's name. Run it through `AssetCatalogDisplayName` before showing it.
    public let name: String
    public let kind: AssetKind
    public let logoUrl: String?
    public let valueUsd: String
    /// This holding's part of `PortfolioDTO.totalUsd`, 0...1, as a decimal string.
    public let shareOfTotal: String
    public let dollarPnl: String
    /// Nil when no cabal can say what its slice cost.
    public let percentReturn: String?
    public let cabals: [PortfolioCabalLineDTO]

    public var id: String { symbol }

    public var logoURL: URL? {
        guard let raw = logoUrl?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else { return nil }
        return URL(string: raw)
    }

    /// How the member knows this stock: "Apple", "SpaceX".
    public var displayName: String {
        AssetCatalogDisplayName.format(catalogName: name, symbol: symbol, kind: kind)
    }

    /// The ticker as the market prints it: "AAPL".
    public var displayTicker: String {
        AssetSymbolFormatter.display(symbol, kind: kind)
    }

    public init(
        symbol: String,
        name: String,
        kind: AssetKind = .stock,
        logoUrl: String? = nil,
        valueUsd: String,
        shareOfTotal: String,
        dollarPnl: String,
        percentReturn: String?,
        cabals: [PortfolioCabalLineDTO]
    ) {
        self.symbol = symbol
        self.name = name
        self.kind = kind
        self.logoUrl = logoUrl
        self.valueUsd = valueUsd
        self.shareOfTotal = shareOfTotal
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
        self.cabals = cabals
    }

    private enum CodingKeys: String, CodingKey {
        case symbol, name, kind, logoUrl, valueUsd, shareOfTotal, dollarPnl, percentReturn, cabals
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        symbol = try container.decode(String.self, forKey: .symbol)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? symbol
        kind = try container.decodeIfPresent(AssetKind.self, forKey: .kind) ?? .stock
        logoUrl = try container.decodeIfPresent(String.self, forKey: .logoUrl)
        valueUsd = try container.decode(String.self, forKey: .valueUsd)
        shareOfTotal = try container.decodeIfPresent(String.self, forKey: .shareOfTotal) ?? "0"
        dollarPnl = try container.decodeIfPresent(String.self, forKey: .dollarPnl) ?? "+0.00"
        percentReturn = try container.decodeIfPresent(String.self, forKey: .percentReturn)
        cabals = try container.decodeIfPresent([PortfolioCabalLineDTO].self, forKey: .cabals) ?? []
    }
}

/// GET /v1/me/portfolio: the member's money in cabals, by stock.
public struct PortfolioDTO: Codable, Equatable, Sendable {
    /// Money in cabals: the same figure Home shows. The account balance is not in it.
    public let totalUsd: String
    /// USDC sitting in the pots, the member's slice of it.
    public let cashUsd: String
    /// USDC in the account, outside every cabal. Nil when the server could not read it.
    public let accountBalanceUsd: String?
    public let dollarPnl: String
    public let percentReturn: String?
    public let holdings: [PortfolioHoldingDTO]
    /// Cabals the server could not value this time. They are left out of every figure.
    public let unvaluedCabals: Int

    public init(
        totalUsd: String,
        cashUsd: String,
        accountBalanceUsd: String?,
        dollarPnl: String,
        percentReturn: String?,
        holdings: [PortfolioHoldingDTO],
        unvaluedCabals: Int = 0
    ) {
        self.totalUsd = totalUsd
        self.cashUsd = cashUsd
        self.accountBalanceUsd = accountBalanceUsd
        self.dollarPnl = dollarPnl
        self.percentReturn = percentReturn
        self.holdings = holdings
        self.unvaluedCabals = unvaluedCabals
    }

    private enum CodingKeys: String, CodingKey {
        case totalUsd, cashUsd, accountBalanceUsd, dollarPnl, percentReturn, holdings, unvaluedCabals
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        totalUsd = try container.decode(String.self, forKey: .totalUsd)
        cashUsd = try container.decodeIfPresent(String.self, forKey: .cashUsd) ?? "0.00"
        accountBalanceUsd = try container.decodeIfPresent(String.self, forKey: .accountBalanceUsd)
        dollarPnl = try container.decodeIfPresent(String.self, forKey: .dollarPnl) ?? "+0.00"
        percentReturn = try container.decodeIfPresent(String.self, forKey: .percentReturn)
        holdings = try container.decodeIfPresent([PortfolioHoldingDTO].self, forKey: .holdings) ?? []
        unvaluedCabals = try container.decodeIfPresent(Int.self, forKey: .unvaluedCabals) ?? 0
    }

    /// Nothing in any cabal: no stock and no cash. The screen says how to start rather than
    /// drawing a $0.00 portfolio.
    public var isEmpty: Bool {
        holdings.isEmpty && PortfolioMath.decimal(cashUsd) <= 0 && PortfolioMath.decimal(totalUsd) <= 0
    }
}

/// One slice of the allocation bar.
public struct PortfolioAllocationSegment: Equatable, Sendable, Identifiable {
    /// The ticker as shown ("AAPL"), or "Cash".
    public let label: String
    public let fraction: Double
    public let isCash: Bool

    public var id: String { isCash ? "cash" : label }

    public init(label: String, fraction: Double, isCash: Bool) {
        self.label = label
        self.fraction = fraction
        self.isCash = isCash
    }
}

public enum PortfolioMath {
    static let posix = Locale(identifier: "en_US_POSIX")

    /// A decimal string from the API as a Decimal; anything unreadable is zero.
    public static func decimal(_ raw: String?) -> Decimal {
        guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty,
              let value = Decimal(string: raw.replacingOccurrences(of: "+", with: ""), locale: posix)
        else { return 0 }
        return value
    }

    /// Each holding's share of the total, largest first, then cash last — the order the
    /// cabal screen's mix bar uses. Slivers under 0.5% are dropped rather than drawn as a
    /// hairline; the rows below still list them.
    public static func allocation(for portfolio: PortfolioDTO) -> [PortfolioAllocationSegment] {
        let total = decimal(portfolio.totalUsd)
        guard total > 0 else { return [] }
        let stocks = portfolio.holdings
            .map { holding -> PortfolioAllocationSegment in
                // The server's own share, so the bar and the API cannot disagree about it.
                let fraction = (decimal(holding.shareOfTotal) as NSDecimalNumber).doubleValue
                return PortfolioAllocationSegment(label: holding.displayTicker, fraction: fraction, isCash: false)
            }
            .filter { $0.fraction >= 0.005 }
            .sorted { $0.fraction > $1.fraction }
        let cashFraction = (decimal(portfolio.cashUsd) / total as NSDecimalNumber).doubleValue
        let cash = cashFraction >= 0.005 ? [PortfolioAllocationSegment(label: "Cash", fraction: cashFraction, isCash: true)] : []
        return stocks + cash
    }

    /// "68%", "<1%": the legend's figure for a segment.
    public static func percentLabel(_ fraction: Double) -> String {
        let value = fraction * 100
        return value < 1 ? "<1%" : String(format: "%.0f%%", value)
    }

    /// "2.4 shares", "1 share", "0.35 tokens" for a slice quantity.
    public static func quantityLabel(_ raw: String, kind: AssetKind) -> String {
        guard let quantity = Decimal(string: raw.trimmingCharacters(in: .whitespacesAndNewlines), locale: posix) else {
            return raw
        }
        return TokenQuantityFormatter.label(quantity: quantity, kind: kind)
    }
}
