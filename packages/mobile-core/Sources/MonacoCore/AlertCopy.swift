import Foundation

/// Every sentence the price-alert screens say, kept out of the views so each one is checked
/// in `AlertCopyTests` and against `MainFlowCopyAudit`.
///
/// A line is written the way a member says it: "Tell me when Google is above $360.00". Prices
/// are the stock's own quote, set in the market's voice by the views.
public enum AlertCopy {
    /// The preset moves the sheet offers, as whole percents of the current price.
    public static let presetPercents = [2, 5, 10]

    /// The highest line the server accepts, in micros.
    public static let maximumLineUsdcMicros: Int64 = 1_000_000 * 1_000_000

    public static let sheetTitle = "Price alert"
    public static let saveTitle = "Save alert"
    public static let customPlaceholder = "Custom price"
    public static let removed = "Alert removed"
    public static let limitReached = "You have 20 alerts waiting. Remove one to set another."
    public static let saveFailed = "Couldn't save the alert. Try again."
    public static let deleteFailed = "Couldn't remove the alert. Try again."
    public static let loadFailed = "Couldn't load your alerts"
    public static let screenTitle = "Price alerts"
    public static let profileRowTitle = "Price alerts"
    public static let emptyTitle = "No price alerts yet"
    public static let emptyMessage = "Open any stock and tap Set alert to hear when it moves."
    public static let profileRowSubtitle = "When a stock crosses your price"
    /// Under "Your alerts on …" when none are waiting.
    public static let emptyListLine = "None waiting yet."
    /// Where the sentence goes before there is a line to say it about.
    public static let formHint = "Pick a move, or type the price you want to hear about."

    /// "Above" or "Below", for the segmented control.
    public static func directionTitle(_ direction: PriceAlertDirection) -> String {
        switch direction {
        case .above: return "Above"
        case .below: return "Below"
        }
    }

    /// "Tell me when Google is above $360.00".
    public static func sentence(name: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) -> String {
        "Tell me when \(name) is \(direction.rawValue) \(UsdAmountFormatter.format(micros: lineUsdcMicros))"
    }

    /// "Above $360.00", the title of an alert's row.
    public static func line(direction: PriceAlertDirection, lineUsdcMicros: Int64) -> String {
        "\(directionTitle(direction)) \(UsdAmountFormatter.format(micros: lineUsdcMicros))"
    }

    /// "Reached $361.25", under a fired alert: the price that set it off.
    public static func reached(priceUsdcMicros: Int64) -> String {
        "Reached \(UsdAmountFormatter.format(micros: priceUsdcMicros))"
    }

    /// "+2%" above, "−2%" below: a typographic minus, as every signed figure in the app has.
    public static func presetLabel(percent: Int, direction: PriceAlertDirection) -> String {
        direction == .above ? "+\(percent)%" : "\u{2212}\(percent)%"
    }

    /// The line a preset puts in the field: the current price moved by `percent`, to the cent.
    public static func presetLine(currentPriceUsdcMicros: Int64, percent: Int, direction: PriceAlertDirection) -> Int64 {
        let factor = Int64(direction == .above ? 100 + percent : 100 - percent)
        let raw = currentPriceUsdcMicros * factor / 100
        return roundedToCents(raw)
    }

    /// Why a line cannot be saved, in the member's words; nil when it can (or when there is
    /// nothing typed yet, which the disabled Save button already says).
    public static func problem(
        name: String,
        direction: PriceAlertDirection,
        currentPriceUsdcMicros: Int64?,
        lineUsdcMicros: Int64?
    ) -> String? {
        guard let line = lineUsdcMicros, line > 0 else { return nil }
        if line > maximumLineUsdcMicros {
            return "Pick a price under $1,000,000."
        }
        guard let current = currentPriceUsdcMicros, current > 0,
              direction.isReached(priceUsdcMicros: current, lineUsdcMicros: line)
        else { return nil }
        let now = UsdAmountFormatter.format(micros: current)
        return "\(name) is at \(now) now. Pick a price \(direction.rawValue) that."
    }

    /// The toast after a save: "We'll tell you when Google is above $360.00".
    public static func created(name: String, direction: PriceAlertDirection, lineUsdcMicros: Int64) -> String {
        "We'll tell you when \(name) is \(direction.rawValue) \(UsdAmountFormatter.format(micros: lineUsdcMicros))"
    }

    /// The toast when the price crossed the line between the sheet and the server.
    public static func alreadyReached(name: String, direction: PriceAlertDirection) -> String {
        "\(name) is already \(direction.rawValue) that price. Pick another."
    }

    /// "Your alerts on Google", over the list in the sheet.
    public static func existingHeader(name: String) -> String {
        "Your alerts on \(name)"
    }

    /// The stock screen's alert button: "Set alert", or how many are waiting.
    public static func buttonTitle(alertCount: Int) -> String {
        switch alertCount {
        case ..<1: return "Set alert"
        case 1: return "1 alert"
        default: return "\(alertCount) alerts"
        }
    }

    public static func buttonAccessibilityLabel(alertCount: Int) -> String {
        switch alertCount {
        case ..<1: return "Set a price alert"
        case 1: return "Price alerts, 1 waiting"
        default: return "Price alerts, \(alertCount) waiting"
        }
    }

    /// "Set 2d ago" for a waiting alert; "Fired 3h ago", then "Fired Sep 25" once it is a day
    /// old, for one that went off.
    public static func stamp(for alert: PriceAlertDTO, now: Date = Date(), calendar: Calendar = .current) -> String {
        if let firedAt = alert.triggeredAt {
            return "Fired \(age(firedAt, now: now, calendar: calendar, daysBeforeDate: 1))"
        }
        return "Set \(age(alert.createdAt, now: now, calendar: calendar, daysBeforeDate: 7))"
    }

    /// The same stamp in words, for VoiceOver: "Set 2 days ago".
    public static func spokenStamp(for alert: PriceAlertDTO, now: Date = Date(), calendar: Calendar = .current) -> String {
        stamp(for: alert, now: now, calendar: calendar)
            .replacingOccurrences(of: #"(\d+)m ago"#, with: "$1 minutes ago", options: .regularExpression)
            .replacingOccurrences(of: #"(\d+)h ago"#, with: "$1 hours ago", options: .regularExpression)
            .replacingOccurrences(of: #"(\d+)d ago"#, with: "$1 days ago", options: .regularExpression)
    }

    /// "just now", "12m ago", "3h ago", "2d ago", then a date. `daysBeforeDate` is how many
    /// whole days still read as an age.
    static func age(_ date: Date, now: Date, calendar: Calendar, daysBeforeDate: Int) -> String {
        let elapsed = max(0, Int(now.timeIntervalSince(date)))
        if elapsed < 60 { return "just now" }
        if elapsed < 3600 { return "\(elapsed / 60)m ago" }
        if elapsed < 86_400 { return "\(elapsed / 3600)h ago" }
        if elapsed < daysBeforeDate * 86_400 { return "\(elapsed / 86_400)d ago" }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return SharedFormatters.string(
            from: date,
            pattern: .fixed(sameYear ? "MMM d" : "MMM d, yyyy"),
            locale: Locale(identifier: "en_US_POSIX"),
            calendar: calendar
        )
    }

    static func roundedToCents(_ micros: Int64) -> Int64 {
        (micros + 5_000) / 10_000 * 10_000
    }

    /// Every fixed string, for the copy audit.
    public static var auditStrings: [String] {
        [
            sheetTitle, saveTitle, customPlaceholder, removed, limitReached, saveFailed, deleteFailed,
            loadFailed, screenTitle, profileRowTitle, emptyTitle, emptyMessage, profileRowSubtitle, emptyListLine, formHint,
            sentence(name: "Alphabet", direction: .above, lineUsdcMicros: 360_000_000),
            line(direction: .below, lineUsdcMicros: 330_000_000),
            reached(priceUsdcMicros: 361_250_000),
            created(name: "Alphabet", direction: .below, lineUsdcMicros: 330_000_000),
            alreadyReached(name: "Alphabet", direction: .above),
            existingHeader(name: "Alphabet"),
            buttonTitle(alertCount: 0), buttonTitle(alertCount: 3),
            buttonAccessibilityLabel(alertCount: 0), buttonAccessibilityLabel(alertCount: 3),
            problem(name: "Alphabet", direction: .above, currentPriceUsdcMicros: 352_100_000, lineUsdcMicros: 350_000_000) ?? "",
        ]
    }
}

/// The watchlist's sentences.
public enum WatchlistCopy {
    public static let sectionTitle = "Watchlist"
    public static let editAction = "Edit"
    public static let editTitle = "Edit watchlist"
    public static let doneAction = "Done"
    public static let added = "Added to your watchlist"
    public static let removed = "Removed from your watchlist"
    public static let full = "Your watchlist is full. Remove a stock to add this one."
    public static let writeFailed = "Couldn't update your watchlist. Try again."
    public static let changed = "Your watchlist changed on another device, so here it is as it stands."
    public static let loadFailed = "Couldn't load your watchlist"
    /// Under the search field, until the member first follows a stock.
    public static let firstUseHint = "Tap the star on any stock to pin it to the top of this tab."
    /// The edit sheet once the last stock is gone.
    public static let editEmptyTitle = "Nothing on your watchlist"
    public static let editEmptyMessage = "Star a stock to add it back."
    public static let removeAction = "Remove from watchlist"

    /// The star's VoiceOver label names what a tap does.
    public static func starLabel(watching: Bool) -> String {
        watching ? "Remove from watchlist" : "Add to watchlist"
    }

    public static var auditStrings: [String] {
        [
            sectionTitle, editAction, editTitle, doneAction, added, removed, full, writeFailed, changed,
            loadFailed, firstUseHint, editEmptyTitle, editEmptyMessage, removeAction, starLabel(watching: true), starLabel(watching: false),
        ]
    }
}
