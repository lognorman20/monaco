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
        try await auth.withAccessToken { try await apiClient.getGroupView(accessToken: $0, groupId: groupId) }
    }
}

/// Each joined cabal's pot, resolved before the member picks one.
///
/// One `getGroupView` per cabal answers both questions the picker has, so neither is asked after
/// the pick: *does this cabal hold the stock* — asking later is what produced the "This cabal does
/// not hold this stock." dead end — and *what is the pot*, which the amount step needs and must
/// not have to load for itself.
///
/// A cabal that does not answer is counted, not hidden behind the ones that did: the picker lists
/// the cabals it has and says how many it could not check. Only when nothing answers at all is
/// there nothing to show. Never claim no cabal holds a stock on a partial answer — that would be a
/// lie about someone's money — but do not withhold the cabal that demonstrably holds it either
/// because an unrelated cabal timed out.
@Observable
@MainActor
final class CabalHoldingsModel {
    /// One cabal that answered: its pot, and its row for this model's symbol when it holds any.
    struct Resolved: Identifiable, Equatable {
        let groupId: String
        let name: String
        let pot: ProposePot
        let holding: PotRowDTO?

        var id: String { groupId }
    }

    enum State: Equatable {
        case loading
        /// The cabals that answered, and how many did not.
        case resolved(cabals: [Resolved], unreachable: Int)
        /// Nothing answered.
        case failed
    }

    let symbol: String
    private(set) var state: State = .loading

    /// Set when the server rejects the session. The data source has already ended it.
    private(set) var sessionExpired = false

    private let dataSource: CabalHoldingsDataSource

    init(symbol: String, dataSource: CabalHoldingsDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    /// The cabals that answered, whichever way.
    var cabals: [Resolved] {
        if case .resolved(let cabals, _) = state { return cabals }
        return []
    }

    /// The cabals that answered *and* hold the stock — the only ones a sell can be proposed from.
    var holders: [Resolved] {
        cabals.filter { $0.holding != nil }
    }

    /// How many cabals could not be checked on the last pass.
    var unreachableCount: Int {
        if case .resolved(_, let unreachable) = state { return unreachable }
        return 0
    }

    private enum Outcome: Sendable {
        case answered(Resolved)
        case unreachable
        case unauthorized
        case cancelled
    }

    func load(cabals: [HomeGroupBoardRowDTO]) async {
        guard !cabals.isEmpty else {
            state = .resolved(cabals: [], unreachable: 0)
            return
        }
        switch state {
        case .resolved: break // Keep the rows on screen while they are re-read.
        case .loading, .failed: state = .loading
        }

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

        let answered = outcomes.compactMap { outcome -> Resolved? in
            if case .answered(let resolved) = outcome { return resolved }
            return nil
        }
        let unreachable = outcomes.reduce(into: 0) { count, outcome in
            if case .unreachable = outcome { count += 1 }
        }
        guard !answered.isEmpty else {
            // Every cabal failed: there is no partial answer to show, only the failure.
            state = .failed
            return
        }
        // Keep the order the cabals were listed in rather than whichever fetch finished first.
        let byId = Dictionary(answered.map { ($0.groupId, $0) }, uniquingKeysWith: { first, _ in first })
        state = .resolved(cabals: cabals.compactMap { byId[$0.groupId] }, unreachable: unreachable)
    }

    private func fetch(cabal: HomeGroupBoardRowDTO) async -> Outcome {
        do {
            let view = try await dataSource.groupView(groupId: cabal.groupId)
            return .answered(Resolved(
                groupId: cabal.groupId,
                name: cabal.name,
                pot: ProposePot(view: view),
                holding: view.pot.first(where: { Self.holds($0, symbol: symbol) })
            ))
        } catch {
            if error.isRequestCancellation { return .cancelled }
            if case MonacoAPIError.httpStatus(401) = error { return .unauthorized }
            return .unreachable
        }
    }

    private static func holds(_ row: PotRowDTO, symbol: String) -> Bool {
        let needle = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
        return row.symbol.uppercased() != "USDC"
            && row.symbol.caseInsensitiveCompare(needle) == .orderedSame
            && (Int64(row.tokenAmount ?? "0") ?? 0) > 0
    }
}
