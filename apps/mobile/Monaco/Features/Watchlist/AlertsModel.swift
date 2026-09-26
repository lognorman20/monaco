import MonacoCore
import Observation
import SwiftUI

/// Every price alert the member has, grouped by stock, for the alerts page.
@Observable
@MainActor
final class AlertsModel {
    enum LoadState: Equatable {
        case loading
        case loaded
        case failed
    }

    private(set) var groups: [PriceAlertGroup] = []
    private(set) var state: LoadState = .loading
    /// A refresh failed with alerts on screen; they stay, captioned as possibly stale.
    private(set) var refreshFailed = false
    var toast: MonacoToast?

    private let dataSource: PriceAlertDataSource

    init(dataSource: PriceAlertDataSource) {
        self.dataSource = dataSource
    }

    var isEmpty: Bool { groups.allSatisfy { $0.alerts.isEmpty } }

    /// A read the member asked for (appear, pull to refresh, Retry).
    func load() async {
        do {
            try await read()
        } catch {
            if error.isRequestCancellation { return }
            if groups.isEmpty {
                state = .failed
            } else {
                refreshFailed = true
            }
        }
    }

    /// The poll: silent about failure, per `pollWhileVisible`.
    func poll() async throws {
        try await read()
    }

    private func read() async throws {
        let response = try await dataSource.alerts(symbol: nil)
        groups = PriceAlertGroup.make(response)
        refreshFailed = false
        state = .loaded
    }

    /// Removes an alert at once and puts it back if the server refuses.
    func delete(_ alert: PriceAlertDTO) async {
        let previous = groups
        groups = groups.compactMap { group in
            guard group.alerts.contains(alert) else { return group }
            let remaining = PriceAlertGroup(
                symbol: group.symbol,
                asset: group.asset,
                waiting: group.waiting.filter { $0 != alert },
                fired: group.fired.filter { $0 != alert }
            )
            return remaining.alerts.isEmpty ? nil : remaining
        }
        do {
            try await dataSource.deleteAlert(id: alert.id)
            toast = MonacoToast(message: AlertCopy.removed, isSuccess: true)
        } catch {
            if error.isRequestCancellation { return }
            if PriceAlertWriteFailure(error) == .notFound { return }
            groups = previous
            toast = MonacoToast(message: AlertCopy.deleteFailed)
        }
    }
}
