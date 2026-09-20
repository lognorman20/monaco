import MonacoCore
import Observation
import SwiftUI

/// Reads for one stock's detail screen. The live source calls the API; tests swap in a stub.
@MainActor
protocol AssetDetailDataSource {
    func detail(symbol: String) async throws -> AssetDetailDTO
    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO
}

@MainActor
struct LiveAssetDetailDataSource: AssetDetailDataSource {
    let auth: PrivyAuthService
    private let apiClient = MonacoAPIClient()

    init(auth: PrivyAuthService) {
        self.auth = auth
    }

    func detail(symbol: String) async throws -> AssetDetailDTO {
        try await auth.withAccessToken { try await apiClient.getMarketAsset(accessToken: $0, symbol: symbol) }
    }

    func chart(symbol: String, range: AssetChartRange) async throws -> AssetChartDTO {
        try await auth.withAccessToken { try await apiClient.getMarketAssetChart(accessToken: $0, symbol: symbol, range: range) }
    }
}

/// State for the stock detail screen.
///
/// The header figure and the curve are read from the same series, and each range owns its own
/// slot, so a slow range can never repaint a range the user has already moved on from.
@Observable
@MainActor
final class AssetDetailModel {
    enum DetailState: Equatable {
        case loading
        case loaded(AssetDetailDTO)
        case failed
    }

    enum ChartState: Equatable {
        case loading
        case series([AssetChartPointDTO])
        case empty
        case failed
    }

    /// The move the header shows, always measured over the window the curve draws.
    struct Move: Equatable {
        /// Backend-style ratio ("0.0124"), so it formats through `PercentReturnFormatter`.
        ///
        /// Always fixed-point. `String(someDouble)` switches to scientific notation below 1e-4
        /// ("5e-05"), and `MonacoTheme.signed` reads the digits of that as "505" and tints a flat
        /// move as a gain.
        let ratio: String
        let label: String

        private var value: Decimal {
            Decimal(string: ratio, locale: Locale(identifier: "en_US_POSIX")) ?? 0
        }

        /// The curve's tint is read from here, never recomputed from the series: a move the
        /// header calls flat must not be drawn in profit green.
        var direction: Direction {
            if value > 0 { return .up }
            if value < 0 { return .down }
            return .flat
        }

        enum Direction: Equatable {
            case up
            case down
            case flat
        }
    }

    let symbol: String
    var range: AssetChartRange = .oneDay
    private(set) var detailState: DetailState = .loading
    private(set) var charts: [AssetChartRange: ChartState] = [:]

    /// Set when the server rejects the session. The data source has already ended it.
    private(set) var sessionExpired = false

    private let dataSource: AssetDetailDataSource

    init(symbol: String, dataSource: AssetDetailDataSource) {
        self.symbol = symbol
        self.dataSource = dataSource
    }

    var detail: AssetDetailDTO? {
        if case .loaded(let detail) = detailState { return detail }
        return nil
    }

    var chartState: ChartState {
        charts[range] ?? .loading
    }

    var chartPoints: [AssetChartPointDTO] {
        if case .series(let points) = chartState { return points }
        return []
    }

    /// Only a definitive Jupiter "no route" blocks the buy; an unknown answer leaves it open.
    var canBuy: Bool {
        detail?.liquidity.routable ?? true
    }

    /// The figure under the price: the drawn window's move when there is a curve, otherwise the
    /// 24h move under its own label so the number never claims a period it did not measure.
    var move: Move? {
        if case .series(let points) = chartState, points.count >= 2,
           let first = points.first?.chartValue, let last = points.last?.chartValue, first > 0 {
            return Move(ratio: Self.ratioString(last / first - 1), label: range.moveLabel)
        }
        guard let change = detail?.change24h, !change.isEmpty else { return nil }
        return Move(ratio: change, label: AssetChartRange.oneDay.moveLabel)
    }

    func loadDetail() async {
        if detail == nil { detailState = .loading }
        do {
            detailState = .loaded(try await dataSource.detail(symbol: symbol))
        } catch {
            handle(error) { if detail == nil { detailState = .failed } }
        }
    }

    /// Writes only into `range`'s slot, so a late response lands where it belongs or nowhere.
    func loadChart(range: AssetChartRange) async {
        if charts[range] == nil || charts[range] == .failed {
            charts[range] = .loading
        }
        do {
            let chart = try await dataSource.chart(symbol: symbol, range: range)
            charts[range] = chart.points.count >= 2 ? .series(chart.points) : .empty
        } catch {
            handle(error) {
                if case .series = charts[range] { return }
                charts[range] = .failed
            }
        }
    }

    /// Fixed-point, POSIX, so the string never reaches the formatters in scientific notation.
    private static func ratioString(_ ratio: Double) -> String {
        String(format: "%.6f", locale: Locale(identifier: "en_US_POSIX"), ratio)
    }

    private func handle(_ error: Error, otherwise: () -> Void) {
        if error.isRequestCancellation { return }
        if case MonacoAPIError.httpStatus(401) = error {
            sessionExpired = true
            return
        }
        otherwise()
    }
}

extension AssetChartRange {
    /// Period the header figure covers. Plain money language, no "24h".
    var moveLabel: String {
        switch self {
        case .oneDay: return "Past day"
        case .oneWeek: return "Past week"
        case .oneMonth: return "Past month"
        case .threeMonths: return "Past three months"
        case .oneYear: return "Past year"
        case .all: return "All time"
        }
    }

    var accessibilityLabel: String {
        switch self {
        case .oneDay: return "One day"
        case .oneWeek: return "One week"
        case .oneMonth: return "One month"
        case .threeMonths: return "Three months"
        case .oneYear: return "One year"
        case .all: return "All time"
        }
    }
}
