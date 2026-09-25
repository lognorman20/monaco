import MonacoCore
import Observation
import SwiftUI

/// Reads one cabal's P&L history. The live source calls the API; the sample harness and tests
/// swap in canned series.
@MainActor
protocol GroupPnLHistorySource {
    func history(groupId: String, range: GroupPnLRange) async throws -> GroupPnLSeriesDTO
}

@MainActor
struct LiveGroupPnLHistorySource: GroupPnLHistorySource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func history(groupId: String, range: GroupPnLRange) async throws -> GroupPnLSeriesDTO {
        try await auth.withAccessToken { try await apiClient.groupPnLHistory(accessToken: $0, groupId: groupId, range: range) }
    }
}

/// What the hero's curve slot holds for the range on screen.
enum GroupHeroChart: Equatable {
    /// Nothing has answered for this range yet.
    case loading
    /// Fewer than three points in the window: a line would read as broken, so the slot says so.
    case sparse
    case failed
    case curve([GroupPnLPointDTO])

    static func resolve(_ series: GroupPnLSeriesDTO?, failed: Bool) -> GroupHeroChart {
        if let series {
            return series.points.count >= 3 ? .curve(series.points) : .sparse
        }
        return failed ? .failed : .loading
    }
}

/// The curve on the cabal hero, one slot per range.
///
/// Each range keeps its own slot, so switching back to a window already drawn is instant and a
/// slow answer for one window can never land under another's chip. A quiet re-read (the cabal
/// screen polls) replaces a slot only when the series actually changed.
@Observable
@MainActor
final class GroupPnLHistoryModel {
    let groupId: String

    var range: GroupPnLRange = .oneMonth

    private(set) var series: [GroupPnLRange: GroupPnLSeriesDTO] = [:]
    private(set) var failedRanges: Set<GroupPnLRange> = []
    private(set) var loadingRanges: Set<GroupPnLRange> = []

    private let source: GroupPnLHistorySource
    private var requestSequence: [GroupPnLRange: Int] = [:]

    init(groupId: String, source: GroupPnLHistorySource) {
        self.groupId = groupId
        self.source = source
    }

    var chart: GroupHeroChart {
        GroupHeroChart.resolve(series[range], failed: failedRanges.contains(range))
    }

    var isLoadingCurrentRange: Bool {
        loadingRanges.contains(range) && series[range] == nil
    }

    /// The move over the drawn window, as the backend's signed dollar string.
    var windowDollarPnl: String? {
        guard case .curve(let points) = chart, let first = points.first, let last = points.last else { return nil }
        let delta = last.chartValue - first.chartValue
        return String(format: "%+.2f", delta)
    }

    func load(range: GroupPnLRange, quietly: Bool = false) async {
        requestSequence[range, default: 0] += 1
        let sequence = requestSequence[range]
        if !quietly { loadingRanges.insert(range) }
        defer { if !quietly { loadingRanges.remove(range) } }
        do {
            let loaded = try await source.history(groupId: groupId, range: range)
            guard requestSequence[range] == sequence, !Task.isCancelled else { return }
            if series[range] != loaded { series[range] = loaded }
            failedRanges.remove(range)
        } catch {
            guard requestSequence[range] == sequence, !error.isRequestCancellation else { return }
            // A quiet re-read that fails leaves the curve the member is looking at alone.
            if quietly, series[range] != nil { return }
            if series[range] == nil { failedRanges.insert(range) }
        }
    }

    func refresh() async {
        await load(range: range, quietly: true)
    }
}
