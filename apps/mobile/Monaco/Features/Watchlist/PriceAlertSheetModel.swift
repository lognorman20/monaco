import MonacoCore
import Observation
import SwiftUI

/// State for setting a price alert on one stock: the side, the line (from a preset or typed),
/// the sentence that line makes, and the alerts already waiting on the stock.
@Observable
@MainActor
final class PriceAlertSheetModel {
    enum ListState: Equatable {
        case loading
        case loaded
        case failed
    }

    let symbol: String
    /// The stock as the member knows it: "Alphabet".
    let name: String
    /// The price the presets are measured from. Kept current by the stock screen's poll.
    var currentPriceUsdcMicros: Int64?

    private(set) var direction: PriceAlertDirection = .above
    private(set) var amountText = ""
    /// The preset the field holds, if it holds one. Flipping the side keeps the same move on
    /// the other side ("+5%" becomes "−5%") instead of leaving a line on the wrong side.
    private(set) var presetPercent: Int?
    /// The alerts waiting on this stock, newest first.
    private(set) var waiting: [PriceAlertDTO] = []
    private(set) var listState: ListState = .loading
    private(set) var isSaving = false
    /// A failure or a removal to show inside the sheet.
    var toast: MonacoToast?

    private let dataSource: PriceAlertDataSource
    /// Tells the stock screen how many alerts now wait, so its button can say so.
    private let onCountChange: (Int) -> Void

    init(
        symbol: String,
        name: String,
        currentPriceUsdcMicros: Int64?,
        dataSource: PriceAlertDataSource,
        onCountChange: @escaping (Int) -> Void = { _ in }
    ) {
        self.symbol = symbol
        self.name = name
        self.currentPriceUsdcMicros = currentPriceUsdcMicros
        self.dataSource = dataSource
        self.onCountChange = onCountChange
    }

    var lineUsdcMicros: Int64? {
        guard let micros = AmountEntryText.micros(amountText), micros > 0 else { return nil }
        return micros
    }

    /// Why Save is off, in words; nil when the line is good or nothing is typed yet.
    var problem: String? {
        AlertCopy.problem(name: name, direction: direction, currentPriceUsdcMicros: currentPriceUsdcMicros, lineUsdcMicros: lineUsdcMicros)
    }

    /// "Tell me when Alphabet is above $360.00", once there is a line to say it about.
    var sentence: String? {
        guard let line = lineUsdcMicros else { return nil }
        return AlertCopy.sentence(name: name, direction: direction, lineUsdcMicros: line)
    }

    var canSave: Bool {
        !isSaving && lineUsdcMicros != nil && problem == nil
    }

    /// The line a preset would put in the field, or nil without a price to measure from.
    func presetLine(_ percent: Int) -> Int64? {
        guard let current = currentPriceUsdcMicros, current > 0 else { return nil }
        return AlertCopy.presetLine(currentPriceUsdcMicros: current, percent: percent, direction: direction)
    }

    func choose(direction newDirection: PriceAlertDirection) {
        guard newDirection != direction else { return }
        direction = newDirection
        if let presetPercent { choose(percent: presetPercent) }
    }

    func choose(percent: Int) {
        guard let line = presetLine(percent) else { return }
        presetPercent = percent
        amountText = AmountEntryText.plain(Decimal(line) / 1_000_000)
    }

    /// Whatever the member types, kept to a price: digits, one point, two decimals.
    func type(_ raw: String) {
        let cleaned = AmountEntryText.sanitize(raw)
        amountText = cleaned
        if let presetPercent, presetLine(presetPercent) != lineUsdcMicros {
            self.presetPercent = nil
        }
    }

    func load() async {
        if waiting.isEmpty { listState = .loading }
        do {
            let response = try await dataSource.alerts(symbol: symbol)
            waiting = response.alerts
                .filter { !$0.hasFired && $0.symbol.caseInsensitiveCompare(symbol) == .orderedSame }
                .sorted { $0.createdAt > $1.createdAt }
            if let price = response.assets.first(where: { $0.symbol.caseInsensitiveCompare(symbol) == .orderedSame })?.priceUsdcMicros,
               currentPriceUsdcMicros == nil {
                currentPriceUsdcMicros = price
            }
            listState = .loaded
            onCountChange(waiting.count)
        } catch {
            if error.isRequestCancellation { return }
            if waiting.isEmpty { listState = .failed }
        }
    }

    /// Saves the line. Returns the toast for the stock screen when it lands; a refusal stays in
    /// the sheet, said in words, and returns nil.
    func save() async -> MonacoToast? {
        guard canSave, let line = lineUsdcMicros else { return nil }
        isSaving = true
        defer { isSaving = false }
        do {
            let alert = try await dataSource.createAlert(symbol: symbol, direction: direction, lineUsdcMicros: line)
            waiting.insert(alert, at: 0)
            onCountChange(waiting.count)
            let saved = MonacoToast(message: AlertCopy.created(name: name, direction: direction, lineUsdcMicros: line), isSuccess: true)
            amountText = ""
            presetPercent = nil
            return saved
        } catch {
            if error.isRequestCancellation { return nil }
            switch PriceAlertWriteFailure(error) {
            case .limitReached:
                toast = MonacoToast(message: AlertCopy.limitReached)
            case .alreadyReached:
                toast = MonacoToast(message: AlertCopy.alreadyReached(name: name, direction: direction))
            case .notFound, .other:
                toast = MonacoToast(message: AlertCopy.saveFailed)
            }
            return nil
        }
    }

    /// Removes a waiting alert at once, and puts it back if the server refuses.
    func delete(_ alert: PriceAlertDTO) async {
        guard let index = waiting.firstIndex(of: alert) else { return }
        waiting.remove(at: index)
        do {
            try await dataSource.deleteAlert(id: alert.id)
            onCountChange(waiting.count)
            toast = MonacoToast(message: AlertCopy.removed, isSuccess: true)
        } catch {
            if error.isRequestCancellation { return }
            // Gone already is what the member asked for.
            if PriceAlertWriteFailure(error) == .notFound {
                onCountChange(waiting.count)
                return
            }
            waiting.insert(alert, at: min(index, waiting.count))
            toast = MonacoToast(message: AlertCopy.deleteFailed)
        }
    }
}
