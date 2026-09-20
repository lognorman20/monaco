import MonacoCore
import Observation
import SwiftUI

/// Reads one cabal's pot, for answering "which of my cabals holds this stock?".
@MainActor
protocol CabalHoldingsDataSource {
    func groupView(groupId: String) async throws -> GroupViewDTO
}

@MainActor
struct LiveCabalHoldingsDataSource: CabalHoldingsDataSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func groupView(groupId: String) async throws -> GroupViewDTO {
        guard let token = auth.accessToken else { throw MonacoAPIError.missingAccessToken }
        return try await apiClient.getGroupView(accessToken: token, groupId: groupId)
    }
}

/// Which joined cabals hold a given stock, resolved before the user picks one.
///
/// Asking after the pick is what produced the "This cabal does not hold this stock." dead end:
/// the answer is known from the same pot payload, so it is read for every cabal up front.
/// A partial answer is treated as no answer — telling someone none of their cabals hold a stock
/// when one fetch failed would be a lie about their money.
@Observable
@MainActor
final class CabalHoldingsModel {
    struct Holding: Identifiable, Equatable {
        let groupId: String
        let name: String
        let row: PotRowDTO

        var id: String { groupId }
    }

    enum State: Equatable {
        case loading
        case loaded([Holding])
        case failed
    }

    let symbol: String
    private(set) var state: State = .loading

    /// Set when the server rejects the session; the view signs out.
    private(set) var sessionExpired = false

    private let dataSource: CabalHoldingsDataSource

    init(symbol: String, dataSource: CabalHoldingsDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    private enum Outcome: Sendable {
        case holds(Holding)
        case doesNotHold
        case unauthorized
        case failed
        case cancelled
    }

    func load(cabals: [HomeGroupBoardRowDTO]) async {
        guard !cabals.isEmpty else {
            state = .loaded([])
            return
        }
        if case .loaded = state {} else { state = .loading }

        let outcomes = await withTaskGroup(of: Outcome.self) { group in
            for cabal in cabals {
                group.addTask { await self.fetch(cabal: cabal) }
            }
            var collected: [Outcome] = []
            for await outcome in group { collected.append(outcome) }
            return collected
        }

        if outcomes.contains(where: { if case .cancelled = $0 { return true } else { return false } }) {
            return
        }
        if outcomes.contains(where: { if case .unauthorized = $0 { return true } else { return false } }) {
            sessionExpired = true
            return
        }
        if outcomes.contains(where: { if case .failed = $0 { return true } else { return false } }) {
            state = .failed
            return
        }
        let holdings = outcomes.compactMap { outcome -> Holding? in
            if case .holds(let holding) = outcome { return holding }
            return nil
        }
        // Keep the order the cabals were listed in rather than whichever fetch finished first.
        let byId = Dictionary(uniqueKeysWithValues: holdings.map { ($0.groupId, $0) })
        state = .loaded(cabals.compactMap { byId[$0.groupId] })
    }

    private func fetch(cabal: HomeGroupBoardRowDTO) async -> Outcome {
        do {
            let view = try await dataSource.groupView(groupId: cabal.groupId)
            guard let row = view.pot.first(where: { Self.holds($0, symbol: symbol) }) else {
                return .doesNotHold
            }
            return .holds(Holding(groupId: cabal.groupId, name: cabal.name, row: row))
        } catch {
            if error.isRequestCancellation { return .cancelled }
            if case MonacoAPIError.httpStatus(401) = error { return .unauthorized }
            return .failed
        }
    }

    private static func holds(_ row: PotRowDTO, symbol: String) -> Bool {
        let needle = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
        return row.symbol.uppercased() != "USDC"
            && row.symbol.caseInsensitiveCompare(needle) == .orderedSame
            && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
    }
}
