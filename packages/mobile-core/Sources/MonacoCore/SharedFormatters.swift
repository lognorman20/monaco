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
        utcTimestamp(raw) ?? iso8601Fractional.date(from: raw) ?? iso8601WholeSeconds.date(from: raw)
    }

    /// `ISO8601DateFormatter.date(from:)` costs ~0.1ms a call, which alone blows a frame on a
    /// busy feed. The backend always sends `yyyy-MM-ddTHH:mm:ss[.fraction]Z`, so that shape is
    /// read with integer math; anything else (offsets, odd spacing) goes to the formatter.
    static func utcTimestamp(_ raw: String) -> Date? {
        let b = Array(raw.utf8)
        guard b.count >= 20, b[4] == 45, b[7] == 45, b[10] == 84, b[13] == 58, b[16] == 58,
              b[b.count - 1] == 90 else { return nil }
        func number(_ from: Int, _ to: Int) -> Int? {
            var value = 0
            for i in from..<to {
                let digit = Int(b[i]) - 48
                guard (0...9).contains(digit) else { return nil }
                value = value * 10 + digit
            }
            return value
        }
        guard let year = number(0, 4), let month = number(5, 7), let day = number(8, 10),
              let hour = number(11, 13), let minute = number(14, 16), let second = number(17, 19),
              (1...12).contains(month), hour < 24, minute < 60, second < 60 else { return nil }
        var fraction = 0.0
        if b.count > 20 {
            guard b[19] == 46, b.count > 21 else { return nil }
            var scale = 0.1
            for i in 20..<(b.count - 1) {
                let digit = Int(b[i]) - 48
                guard (0...9).contains(digit) else { return nil }
                fraction += Double(digit) * scale
                scale /= 10
            }
        }
        let leap = (year % 4 == 0 && year % 100 != 0) || year % 400 == 0
        let monthDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
        guard day >= 1, day <= monthDays[month - 1] else { return nil }
        // Days from civil (Howard Hinnant): proleptic Gregorian date to days since 1970-01-01.
        let y = month <= 2 ? year - 1 : year
        let era = (y >= 0 ? y : y - 399) / 400
        let yoe = y - era * 400
        let doy = (153 * (month + (month > 2 ? -3 : 9)) + 2) / 5 + day - 1
        let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy
        let days = era * 146_097 + doe - 719_468
        let seconds = days * 86_400 + hour * 3600 + minute * 60 + second
        return Date(timeIntervalSince1970: Double(seconds) + fraction)
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
