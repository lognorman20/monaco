import Foundation

enum MarketFormatters {
    static func usd(fromMicros micros: Int64?) -> String {
        guard let micros else { return "—" }
        let decimal = Decimal(micros) / Decimal(1_000_000)
        var rounded = Decimal()
        var source = decimal
        NSDecimalRound(&rounded, &source, 2, .plain)
        let number = rounded as NSDecimalNumber
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.locale = Locale(identifier: "en_US_POSIX")
        return formatter.string(from: number) ?? "$\(number)"
    }

    static func percentChange(_ raw: String?) -> String? {
        guard let raw, !raw.isEmpty, let value = Double(raw) else { return nil }
        let pct = value * 100
        let prefix = pct >= 0 ? "+" : ""
        return String(format: "\(prefix)%.2f%%", pct)
    }
}
