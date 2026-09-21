import Foundation
import MonacoCore

/// A stock the propose flow can show: catalog search rows carry no price, popular rows do.
struct ProposeStock: Hashable, Identifiable {
    let symbol: String
    let name: String
    var priceMicros: Int64?
    var change24h: String?
    var isTradable = true

    var id: String { symbol }

    /// Ticker without the xStock suffix, e.g. "AAPL".
    var ticker: String { AssetSymbolFormatter.display(symbol) }

    var priceUsd: Decimal? {
        priceMicros.map { Decimal($0) / Decimal(1_000_000) }
    }

    init(symbol: String, name: String, priceMicros: Int64? = nil, change24h: String? = nil, isTradable: Bool = true) {
        self.symbol = symbol
        self.name = name
        self.priceMicros = priceMicros
        self.change24h = change24h
        self.isTradable = isTradable
    }

    /// Name from the static table first, then the catalog name, then the ticker.
    static func displayName(symbol: String, catalogName: String? = nil) -> String {
        if let known = AssetDisplayNames.name(forSymbol: symbol) { return known }
        if let catalogName, !catalogName.isEmpty { return AssetDisplayName.format(catalogName: catalogName) }
        return AssetSymbolFormatter.display(symbol)
    }

    init(market: MarketAssetDTO) {
        self.init(
            symbol: market.symbol,
            name: Self.displayName(symbol: market.symbol, catalogName: market.name),
            priceMicros: market.priceUsdcMicros,
            change24h: market.change24h,
            isTradable: market.canBuy
        )
    }

    init(catalog: CatalogAssetDTO) {
        self.init(
            symbol: catalog.symbol,
            name: Self.displayName(symbol: catalog.symbol, catalogName: catalog.name),
            isTradable: catalog.isTradable
        )
    }

    init(symbol: String) {
        self.init(symbol: symbol, name: Self.displayName(symbol: symbol))
    }
}

/// What the viewer asks the cabal to vote on.
enum ProposalDraft: Equatable {
    case buy(symbol: String, usdcMicros: Int64, thesis: String)
    case sell(symbol: String, tokenAmount: Int64, thesis: String)
    case addAgent(name: String, allocationMicros: Int64)
    case agentLifecycle(kind: String)
}

/// The pot numbers the propose screens need, read once from the cabal view.
struct ProposePot: Equatable {
    let groupId: String
    let name: String
    /// Buy ceiling: the whole pot (cash plus holdings), matching the backend rule.
    let totalMicros: Int64
    let holdings: [PotRowDTO]

    init(view: GroupViewDTO) {
        groupId = view.id
        name = view.name
        totalMicros = ProposeMath.micros(fromUsd: view.resolvedPotTotalUsd) ?? 0
        holdings = view.pot.filter { row in
            row.symbol.uppercased() != "USDC" && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
        }
    }
}

/// Backend calls behind the propose chooser and flows. Views depend on this protocol so the flows
/// run against the API or, in Debug, against in-memory sample data.
@MainActor
protocol ProposeService: AnyObject {
    func pot(groupId: String) async throws -> ProposePot
    func popularStocks() async throws -> [ProposeStock]
    func searchStocks(groupId: String, query: String, offset: Int, limit: Int) async throws -> (stocks: [ProposeStock], hasMore: Bool)
    /// Latest price per share in USDC micros, nil when the market has none.
    func priceMicros(symbol: String) async throws -> Int64?
    func buyQuote(groupId: String, symbol: String, usdcMicros: Int64) async throws -> BuyQuoteDTO
    func sellQuote(groupId: String, symbol: String, tokenAmount: Int64) async throws -> BuyQuoteDTO
    /// Creates the proposal and returns its id.
    func propose(groupId: String, draft: ProposalDraft) async throws -> String
}

@MainActor
final class LiveProposeService: ProposeService {
    private weak var auth: DynamicAuthService?
    private let client = MonacoAPIClient()

    init(auth: DynamicAuthService) {
        self.auth = auth
    }

    private func token() throws -> String {
        guard let token = auth?.accessToken else { throw MonacoAPIError.missingAccessToken }
        return token
    }

    func pot(groupId: String) async throws -> ProposePot {
        ProposePot(view: try await client.getGroupView(accessToken: try token(), groupId: groupId))
    }

    func popularStocks() async throws -> [ProposeStock] {
        try await client.getPopularAssets(accessToken: try token(), limit: 10).assets.map(ProposeStock.init(market:))
    }

    func searchStocks(groupId: String, query: String, offset: Int, limit: Int) async throws -> (stocks: [ProposeStock], hasMore: Bool) {
        let response = try await client.searchAssets(
            accessToken: try token(), groupId: groupId, query: query, limit: limit, offset: offset
        )
        return (response.assets.map(ProposeStock.init(catalog:)), response.hasMore)
    }

    func priceMicros(symbol: String) async throws -> Int64? {
        try await client.getMarketAsset(accessToken: try token(), symbol: symbol).priceUsdcMicros
    }

    func buyQuote(groupId: String, symbol: String, usdcMicros: Int64) async throws -> BuyQuoteDTO {
        try await client.postQuote(accessToken: try token(), groupId: groupId, symbol: symbol, kind: "buy", usdc: usdcMicros)
    }

    func sellQuote(groupId: String, symbol: String, tokenAmount: Int64) async throws -> BuyQuoteDTO {
        try await client.postQuote(
            accessToken: try token(), groupId: groupId, symbol: symbol, kind: "sell", usdc: nil, tokenAmount: tokenAmount
        )
    }

    func propose(groupId: String, draft: ProposalDraft) async throws -> String {
        let token = try token()
        let response: CreateProposalResponse
        switch draft {
        case let .buy(symbol, usdcMicros, thesis):
            response = try await client.createProposal(
                accessToken: token, groupId: groupId, kind: "buy", symbol: symbol, usdcMicros: usdcMicros, thesis: thesis.isEmpty ? nil : thesis
            )
        case let .sell(symbol, tokenAmount, thesis):
            response = try await client.createProposal(
                accessToken: token, groupId: groupId, kind: "sell", symbol: symbol, tokenAmount: tokenAmount, thesis: thesis.isEmpty ? nil : thesis
            )
        case let .addAgent(name, allocationMicros):
            response = try await client.createProposal(
                accessToken: token, groupId: groupId, kind: "add_agent",
                agentDisplayName: name, allocationUsdcMicros: allocationMicros
            )
        case let .agentLifecycle(kind):
            response = try await client.createProposal(accessToken: token, groupId: groupId, kind: kind)
        }
        return response.proposalId
    }
}

/// Maps propose errors to one sentence a member can act on. Never shows status codes or
/// `localizedDescription`.
enum ProposeErrorCopy {
    static func quote(_ error: Error) -> String {
        isOffline(error) ? ProposeFlowCopy.noConnection : ProposeFlowCopy.priceCheckFailed
    }

    static func propose(_ error: Error, stockName: String? = nil) -> String {
        if isOffline(error) { return ProposeFlowCopy.noConnection }
        switch error {
        case MonacoAPIError.apiError(_, let message):
            switch message {
            case "amount exceeds treasury total available": return ProposeFlowCopy.overPot
            case "thesis exceeds maximum length": return ProposeFlowCopy.reasonTooLong
            case "quote not routable":
                return stockName.map(ProposeFlowCopy.cantBuyStock) ?? ProposeFlowCopy.sendFailed
            default: return ProposeFlowCopy.sendFailed
            }
        default:
            return ProposeFlowCopy.sendFailed
        }
    }

    static func sell(_ error: Error) -> String {
        if isOffline(error) { return ProposeFlowCopy.noConnection }
        switch error {
        case MonacoAPIError.apiError(_, "thesis exceeds maximum length"):
            return ProposeFlowCopy.reasonTooLong
        case MonacoAPIError.httpStatus(400), MonacoAPIError.apiError(400, _):
            return ProposeFlowCopy.sellNoLongerAvailable
        default:
            return ProposeFlowCopy.sendFailed
        }
    }

    private static func isOffline(_ error: Error) -> Bool {
        guard let urlError = error as? URLError else { return false }
        return [.notConnectedToInternet, .networkConnectionLost, .timedOut, .cannotConnectToHost].contains(urlError.code)
    }
}

/// Fixed-point conversions for the propose flows. USDC has 6 decimals; xStock tokens have 8.
enum ProposeMath {
    static let usdcScale = Decimal(1_000_000)
    static let shareScale = Decimal(sign: .plus, exponent: ProposalShareFormatter.decimals, significand: 1)

    static func micros(fromUsd raw: String) -> Int64? {
        guard let value = Decimal(string: raw.trimmingCharacters(in: .whitespaces), locale: Locale(identifier: "en_US_POSIX")) else {
            return nil
        }
        return micros(fromUsd: value)
    }

    static func micros(fromUsd value: Decimal) -> Int64? {
        guard value >= 0 else { return nil }
        return rounded(value * usdcScale, mode: .plain)
    }

    static func usd(fromMicros micros: Int64) -> Decimal {
        Decimal(micros) / usdcScale
    }

    /// Amount text from the decimal pad, in USDC micros. Nil for empty, zero, or unreadable input.
    static func micros(fromAmountText text: String) -> Int64? {
        let trimmed = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
        guard !trimmed.isEmpty, let value = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")), value > 0,
              let micros = micros(fromUsd: value), micros > 0 else { return nil }
        return micros
    }

    static func shares(fromAtomics raw: String) -> Decimal? {
        guard let atomics = Decimal(string: raw, locale: Locale(identifier: "en_US_POSIX")) else { return nil }
        return atomics / shareScale
    }

    /// Token atomics for a dollar amount of a holding at its mark, rounded down so a sell never
    /// asks for more than the cabal holds.
    static func atomics(forUsd usd: Decimal, markUsd: Decimal, ceiling: Int64) -> Int64? {
        guard markUsd > 0, usd > 0 else { return nil }
        let raw = rounded(usd / markUsd * shareScale, mode: .down) ?? 0
        let clamped = min(raw, ceiling)
        return clamped > 0 ? clamped : nil
    }

    static func atomics(fromShares text: String) -> Int64? {
        let trimmed = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
        guard let value = Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX")), value > 0 else { return nil }
        let atomics = rounded(value * shareScale, mode: .down) ?? 0
        return atomics > 0 ? atomics : nil
    }

    private static func rounded(_ value: Decimal, mode: NSDecimalNumber.RoundingMode) -> Int64? {
        var source = value
        var result = Decimal()
        NSDecimalRound(&result, &source, 0, mode)
        return (result as NSDecimalNumber).int64Value
    }
}
