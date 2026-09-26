import MonacoCore
import Observation
import SwiftUI

/// The stock screen's star and alert count.
///
/// Both arrive on the detail read the screen already polls, so this model adds no fetch of its
/// own: it absorbs `watching` and `alertCount` from each detail, except while one of the
/// member's own changes is settling — a poll that left before the tap would otherwise flip the
/// star back.
@Observable
@MainActor
final class AssetWatchModel {
    /// How long a local change outranks the polled detail: longer than one poll plus its read.
    static let localChangeGrace: TimeInterval = 15

    let symbol: String
    /// Nil until the first detail says; the star waits for it.
    private(set) var watching: Bool?
    private(set) var alertCount = 0
    private(set) var isSaving = false

    private let dataSource: WatchlistDataSource
    private let clock: () -> Date
    private var lastLocalChange: Date?

    init(symbol: String, dataSource: WatchlistDataSource, clock: @escaping () -> Date = Date.init) {
        self.symbol = symbol
        self.dataSource = dataSource
        self.clock = clock
    }

    /// Takes the member's state from a detail read, unless their own change is still settling.
    func absorb(_ detail: AssetDetailDTO?) {
        guard let detail else { return }
        if isSaving { return }
        if let lastLocalChange, clock().timeIntervalSince(lastLocalChange) < Self.localChangeGrace { return }
        if let watching = detail.watching { self.watching = watching }
        if let count = detail.alertCount { alertCount = max(0, count) }
    }

    /// Stars or unstars the stock. The star moves at once; a refusal puts it back. Returns the
    /// toast to show either way.
    func toggle() async -> MonacoToast? {
        guard !isSaving else { return nil }
        let wasWatching = watching ?? false
        isSaving = true
        watching = !wasWatching
        lastLocalChange = clock()
        defer { isSaving = false }
        do {
            if wasWatching {
                try await dataSource.remove(symbol: symbol)
            } else {
                try await dataSource.add(symbol: symbol)
            }
            lastLocalChange = clock()
            NotificationCenter.default.post(name: .monacoWatchlistDidChange, object: nil)
            return MonacoToast(message: wasWatching ? WatchlistCopy.removed : WatchlistCopy.added, isSuccess: true)
        } catch {
            watching = wasWatching
            lastLocalChange = nil
            if error.isRequestCancellation { return nil }
            switch WatchlistWriteFailure(error) {
            case .full: return MonacoToast(message: WatchlistCopy.full)
            default: return MonacoToast(message: WatchlistCopy.writeFailed)
            }
        }
    }

    /// The alert sheet reports how many alerts now wait on this stock.
    func alertsChanged(count: Int) {
        alertCount = max(0, count)
        lastLocalChange = clock()
    }
}

/// The star in the stock screen's navigation bar. Filled when the stock is on the watchlist.
struct WatchStarButton: View {
    let watching: Bool?
    let isSaving: Bool
    let action: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var isOn: Bool { watching ?? false }

    var body: some View {
        Button(action: action) {
            Image(systemName: isOn ? "star.fill" : "star")
                .font(.body.weight(.semibold))
                .foregroundStyle(watching == nil ? MonacoTheme.disabledLabel : MonacoTheme.brand)
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(watching == nil || isSaving)
        .accessibilityLabel(WatchlistCopy.starLabel(watching: isOn))
        .accessibilityAddTraits(isOn ? .isSelected : [])
        .accessibilityIdentifier("asset-watch-button")
    }
}

/// "Set alert", or how many wait, under the stock's price. Drawn as the app's secondary button
/// (paper capsule, hairline) in the interactive ink, so it cannot be mistaken for the session
/// chip above it, which is a status on sunken paper.
struct PriceAlertButton: View {
    let alertCount: Int
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Image(systemName: alertCount > 0 ? "bell.fill" : "bell")
                    .font(MonacoTheme.Typo.captionStrong)
                Text(AlertCopy.buttonTitle(alertCount: alertCount))
                    .font(MonacoTheme.Typo.calloutStrong)
                    .lineLimit(1)
            }
            .foregroundStyle(MonacoTheme.brand)
            .padding(.horizontal, 14)
            .frame(minHeight: 36)
            .background(Capsule().fill(MonacoTheme.secondaryButtonFill))
            .overlay { Capsule().strokeBorder(MonacoTheme.hairline, lineWidth: 1) }
            .padding(.vertical, 4)
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(AlertCopy.buttonAccessibilityLabel(alertCount: alertCount))
        .accessibilityIdentifier("asset-alert-button")
    }
}
