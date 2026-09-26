#if DEBUG
import MonacoCore
import SwiftUI

/// Debug-only: the portfolio and history screens from canned data, so QA can screenshot every
/// state without Privy or a backend. Launch with
/// `-MonacoPortfolioSample <portfolio|portfolioEmpty|portfolioLoading|portfolioFailed|history|historyFiltered|historyEmpty|historyLoading|historyFailed>`.
enum PortfolioSampleScenario: String, CaseIterable {
    case portfolio
    case portfolioEmpty
    case portfolioLoading
    case portfolioFailed
    case history
    case historyFiltered
    case historyEmpty
    case historyLoading
    case historyFailed

    static var requested: PortfolioSampleScenario? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let flag = arguments.firstIndex(of: "-MonacoPortfolioSample"),
              arguments.indices.contains(flag + 1)
        else { return nil }
        return PortfolioSampleScenario(rawValue: arguments[flag + 1])
    }
}

struct PortfolioSampleHarness: View {
    let scenario: PortfolioSampleScenario
    @ObservedObject var auth: PrivyAuthService
    @State private var session = AppSessionStore()

    var body: some View {
        NavigationStack {
            if scenario.rawValue.hasPrefix("history") {
                HistoryView(auth: auth, service: service, model: historyModel)
            } else {
                PortfolioView(auth: auth, service: service, model: portfolioModel)
            }
        }
        .environment(session)
    }

    private var service: SamplePortfolioService {
        switch scenario {
        case .portfolioLoading, .historyLoading: return SamplePortfolioService(mode: .hang)
        case .portfolioFailed, .historyFailed: return SamplePortfolioService(mode: .offline)
        case .portfolioEmpty, .historyEmpty: return SamplePortfolioService(mode: .answer(PortfolioSampleData.emptyPortfolio, []))
        default: return SamplePortfolioService(mode: .answer(PortfolioSampleData.portfolio, PortfolioSampleData.history()))
        }
    }

    /// The populated portfolio opens with Apple showing its two cabals, so the screenshot shows
    /// a stock held across cabals.
    private var portfolioModel: PortfolioModel? {
        guard scenario == .portfolio else { return nil }
        return PortfolioModel(service: service, state: .loaded(PortfolioSampleData.portfolio), expanded: ["AAPLx"])
    }

    private var historyModel: HistoryModel? {
        guard scenario == .historyFiltered else { return nil }
        let buys = PortfolioSampleData.history().filter { ["buy", "bot_buy"].contains($0.kind) }
        return HistoryModel(service: service, filter: .buy, phase: .loaded, items: buys)
    }
}

/// Stands in for the backend: answers with canned data, never answers, or fails like a phone
/// with no signal.
@MainActor
struct SamplePortfolioService: PortfolioService {
    enum Mode {
        case answer(PortfolioDTO, [HistoryItemDTO])
        case hang
        case offline
    }

    let mode: Mode

    func portfolio() async throws -> PortfolioDTO {
        switch mode {
        case .answer(let portfolio, _): return portfolio
        case .hang: return try await hang()
        case .offline: throw URLError(.notConnectedToInternet)
        }
    }

    func history(filter: HistoryFilter, cursor: String?) async throws -> HistoryPageDTO {
        switch mode {
        case .answer(_, let items):
            return HistoryPageDTO(items: items.filter { Self.keeps(filter, $0) }, nextCursor: nil)
        case .hang: return try await hang()
        case .offline: throw URLError(.notConnectedToInternet)
        }
    }

    func exportCSV(filter: HistoryFilter) async throws -> Data {
        let page = try await history(filter: filter, cursor: nil)
        let header = "date,kind,status,cabal,stock,amount_usd,quantity,id"
        let rows = page.items.map { item in
            [
                ISO8601DateFormatter().string(from: item.at), item.kind, item.status,
                item.groupName ?? "", item.symbol ?? "", item.amountUsd ?? "", item.quantity ?? "", item.id,
            ].joined(separator: ",")
        }
        return Data(([header] + rows).joined(separator: "\n").utf8)
    }

    private static func keeps(_ filter: HistoryFilter, _ item: HistoryItemDTO) -> Bool {
        switch filter {
        case .all: return true
        case .moneyIn: return item.resolvedKind.isMoneyIn
        case .cashOut: return item.resolvedKind == .cashOut || item.resolvedKind == .withdrawal
        case .buy: return item.resolvedKind == .buy || item.resolvedKind == .botBuy
        case .sell: return item.resolvedKind == .sell || item.resolvedKind == .botSell
        }
    }

    private func hang<T>() async throws -> T {
        try await Task.sleep(for: .seconds(3600))
        throw CancellationError()
    }
}

/// The member from the Home and Profile samples: the same three cabals, the same $248.50
/// account balance.
enum PortfolioSampleData {
    static let weekend = (id: "g1", name: "Weekend investors")
    static let semis = (id: "g2", name: "Semis or bust")
    static let index = (id: "g3", name: "Index huggers")

    static let portfolio = PortfolioDTO(
        totalUsd: "1758.70",
        cashUsd: "220.00",
        accountBalanceUsd: "248.50",
        dollarPnl: "+158.70",
        percentReturn: "0.099188",
        holdings: [
            PortfolioHoldingDTO(
                symbol: "AAPLx", name: "Apple xStock", valueUsd: "850.00", shareOfTotal: "0.483311",
                dollarPnl: "+170.00", percentReturn: "0.25",
                cabals: [
                    PortfolioCabalLineDTO(groupId: weekend.id, name: weekend.name, valueUsd: "600.00", quantity: "2.4", dollarPnl: "+120.00"),
                    PortfolioCabalLineDTO(groupId: semis.id, name: semis.name, valueUsd: "250.00", quantity: "1", dollarPnl: "+50.00"),
                ]
            ),
            PortfolioHoldingDTO(
                symbol: "NVDAx", name: "NVIDIA xStock", valueUsd: "412.30", shareOfTotal: "0.234435",
                dollarPnl: "+62.30", percentReturn: "0.178",
                cabals: [
                    PortfolioCabalLineDTO(groupId: semis.id, name: semis.name, valueUsd: "412.30", quantity: "2.35", dollarPnl: "+62.30"),
                ]
            ),
            PortfolioHoldingDTO(
                symbol: "TSLAx", name: "Tesla xStock", valueUsd: "180.00", shareOfTotal: "0.102348",
                dollarPnl: "-20.00", percentReturn: "-0.1",
                cabals: [
                    PortfolioCabalLineDTO(groupId: semis.id, name: semis.name, valueUsd: "180.00", quantity: "2", dollarPnl: "-20.00"),
                ]
            ),
            PortfolioHoldingDTO(
                symbol: "tSpaceX", name: "T-SpaceX", kind: .preIpo, valueUsd: "96.40", shareOfTotal: "0.054813",
                dollarPnl: "+6.40", percentReturn: "0.071111",
                cabals: [
                    PortfolioCabalLineDTO(groupId: index.id, name: index.name, valueUsd: "96.40", quantity: "0.52", dollarPnl: "+6.40"),
                ]
            ),
        ]
    )

    /// No cabal money yet, but $248.50 waiting in the account.
    static let emptyPortfolio = PortfolioDTO(
        totalUsd: "0.00",
        cashUsd: "0.00",
        accountBalanceUsd: "248.50",
        dollarPnl: "+0.00",
        percentReturn: nil,
        holdings: []
    )

    /// Ten rows over five days, one of every kind and state, anchored to today so the day
    /// headings read Today, Yesterday, then dates.
    static func history(now: Date = Date(), calendar: Calendar = .current) -> [HistoryItemDTO] {
        let today = calendar.startOfDay(for: now)
        func at(daysAgo: Int, hour: Int, minute: Int) -> Date {
            let day = calendar.date(byAdding: .day, value: -daysAgo, to: today) ?? today
            return calendar.date(byAdding: .minute, value: hour * 60 + minute, to: day) ?? day
        }
        return [
            HistoryItemDTO(id: "h01", kind: "fund", status: "pending", groupId: semis.id, groupName: semis.name,
                           amountUsd: "50.00", at: at(daysAgo: 0, hour: 9, minute: 12)),
            HistoryItemDTO(id: "h02", kind: "buy", status: "done", groupId: weekend.id, groupName: weekend.name,
                           symbol: "AAPLx", name: "Apple xStock", assetKind: .stock, amountUsd: "480.00", quantity: "2.4",
                           at: at(daysAgo: 0, hour: 8, minute: 41), transactionId: "tx-02"),
            HistoryItemDTO(id: "h03", kind: "bot_sell", status: "done", groupId: semis.id, groupName: semis.name,
                           symbol: "NVDAx", name: "NVIDIA xStock", assetKind: .stock, amountUsd: "156.00", quantity: "0.6",
                           at: at(daysAgo: 1, hour: 15, minute: 30), transactionId: "tx-03"),
            HistoryItemDTO(id: "h04", kind: "cash_out", status: "pending", groupId: index.id, groupName: index.name,
                           amountUsd: "70.00", at: at(daysAgo: 1, hour: 11, minute: 5)),
            HistoryItemDTO(id: "h05", kind: "bot_buy", status: "done", groupId: semis.id, groupName: semis.name,
                           symbol: "NVDAx", name: "NVIDIA xStock", assetKind: .stock, amountUsd: "350.00", quantity: "2.95",
                           at: at(daysAgo: 1, hour: 10, minute: 0), transactionId: "tx-05"),
            HistoryItemDTO(id: "h06", kind: "buy", status: "failed", groupId: semis.id, groupName: semis.name,
                           symbol: "TSLAx", name: "Tesla xStock", assetKind: .stock, amountUsd: "30.00",
                           at: at(daysAgo: 3, hour: 16, minute: 20), transactionId: "tx-06"),
            HistoryItemDTO(id: "h07", kind: "cash_out", status: "failed", groupId: semis.id, groupName: semis.name,
                           amountUsd: "40.00", at: at(daysAgo: 3, hour: 14, minute: 2)),
            HistoryItemDTO(id: "h08", kind: "withdrawal", status: "done",
                           amountUsd: "25.00", at: at(daysAgo: 3, hour: 9, minute: 48)),
            HistoryItemDTO(id: "h09", kind: "buy", status: "done", groupId: index.id, groupName: index.name,
                           symbol: "tSpaceX", name: "T-SpaceX", assetKind: .preIpo, amountUsd: "90.00", quantity: "0.52",
                           at: at(daysAgo: 4, hour: 13, minute: 15), transactionId: "tx-09"),
            HistoryItemDTO(id: "h10", kind: "fund", status: "done", groupId: weekend.id, groupName: weekend.name,
                           amountUsd: "600.00", at: at(daysAgo: 4, hour: 10, minute: 30)),
        ]
    }
}
#endif
