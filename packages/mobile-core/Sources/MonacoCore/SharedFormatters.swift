import Foundation

/// Foundation formatters are expensive to build (each one loads ICU data) and cheap to use.
/// Labels are formatted once per visible row on every SwiftUI body pass, so building a
/// formatter per call turns one screen of money and dates into more than a frame of
/// main-thread work. These are built once and only ever read afterwards.
///
/// `ISO8601DateFormatter`, `NumberFormatter` and `DateFormatter` are safe to use from several
/// threads as long as nobody mutates them, which is why none of these are handed out for
/// configuration: callers get a finished formatter or a finished string.
public enum SharedFormatters {
    // MARK: - ISO-8601

    /// `2026-09-18T15:04:05.123Z`
    public static let iso8601Fractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    /// `2026-09-18T15:04:05Z`
    public static let iso8601WholeSeconds: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    /// Backend timestamps come with or without fractional seconds; accept both.
    public static func iso8601Date(from raw: String) -> Date? {
        iso8601Fractional.date(from: raw) ?? iso8601WholeSeconds.date(from: raw)
    }

    // MARK: - Calendar dates

    /// How a cached `DateFormatter` spells its pattern.
    public enum DatePattern: Hashable, Sendable {
        /// A fixed `dateFormat`, e.g. "MMM d".
        case fixed(String)
        /// A template localised for the formatter's locale, e.g. "MMMd".
        case template(String)
    }

    private struct DateFormatterKey: Hashable {
        let pattern: DatePattern
        let localeIdentifier: String
        let calendarIdentifier: Calendar.Identifier
        let timeZoneIdentifier: String
    }

    private static let dateFormatterLock = NSLock()
    private static var dateFormatters: [DateFormatterKey: DateFormatter] = [:]

    /// One formatter per (pattern, locale, calendar, time zone). A device only ever asks for a
    /// handful of combinations, so the cache stays tiny.
    public static func string(
        from date: Date,
        pattern: DatePattern,
        locale: Locale,
        calendar: Calendar = .current,
        timeZone: TimeZone? = nil
    ) -> String {
        let zone = timeZone ?? calendar.timeZone
        let key = DateFormatterKey(
            pattern: pattern,
            localeIdentifier: locale.identifier,
            calendarIdentifier: calendar.identifier,
            timeZoneIdentifier: zone.identifier
        )
        dateFormatterLock.lock()
        defer { dateFormatterLock.unlock() }
        if let cached = dateFormatters[key] {
            return cached.string(from: date)
        }
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.calendar = calendar
        formatter.timeZone = zone
        switch pattern {
        case .fixed(let format): formatter.dateFormat = format
        case .template(let template): formatter.setLocalizedDateFormatFromTemplate(template)
        }
        dateFormatters[key] = formatter
        return formatter.string(from: date)
    }
}
