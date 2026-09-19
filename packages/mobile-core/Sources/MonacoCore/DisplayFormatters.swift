import Foundation

public enum PercentReturnFormatter {
    /// Formats a backend return ratio ("0.124", "-0.036") as "+12.4%" / "-3.6%".
    /// Strings that are already percentages ("+12.4%") pass through.
    public static func format(_ raw: String?) -> String {
        guard let raw, !raw.isEmpty else { return "—" }
        if raw.hasSuffix("%") { return raw }
        if let value = Double(raw) {
            let pct = value * 100
            let prefix = pct >= 0 ? "+" : ""
            return String(format: "\(prefix)%.1f%%", pct)
        }
        return raw
    }
}

public enum DollarPnlFormatter {
    public static func format(_ raw: String) -> String {
        if raw.hasPrefix("-") {
            return "\(raw) loss"
        }
        return raw
    }
}

public enum SlicePercentFormatter {
    public static func format(_ raw: String) -> String {
        guard let value = Double(raw) else { return raw }
        return String(format: "%.1f%%", value * 100)
    }
}

public enum AssetSymbolFormatter {
    /// User-facing ticker; never show raw Solana mint as primary label.
    public static func format(_ symbol: String) -> String {
        let trimmed = symbol.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return trimmed }
        if looksLikeSolanaMint(trimmed) {
            return "Unknown stock"
        }
        return trimmed
    }

    private static func looksLikeSolanaMint(_ value: String) -> Bool {
        guard (32...44).contains(value.count) else { return false }
        let base58 = CharacterSet(charactersIn: "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")
        return value.unicodeScalars.allSatisfy { base58.contains($0) }
    }
}

public enum UsdAmountFormatter {
    private static let posix = Locale(identifier: "en_US_POSIX")

    public static func format(decimalString: String) -> String {
        guard let decimal = Decimal(string: decimalString, locale: posix) else {
            return "$\(decimalString)"
        }
        return format(decimal: decimal)
    }

    public static func format(micros: Int64) -> String {
        format(decimal: Decimal(micros) / Decimal(1_000_000))
    }

    public static func format(decimal: Decimal) -> String {
        var rounded = Decimal()
        var source = decimal
        NSDecimalRound(&rounded, &source, 2, .plain)
        let number = rounded as NSDecimalNumber
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        formatter.groupingSeparator = ","
        formatter.usesGroupingSeparator = true
        formatter.locale = posix
        let body = formatter.string(from: number) ?? number.stringValue
        return "$\(body)"
    }
}

/// Converts between a member's deployed stake (USD NAV) and share micros for withdraw APIs.
public enum StakeWithdrawConverter {
    private static let posix = Locale(identifier: "en_US_POSIX")

    public static func usdMicros(fromDecimalString equityUsd: String) -> Int64? {
        guard let decimal = Decimal(string: equityUsd, locale: posix), decimal >= 0 else {
            return nil
        }
        var scaled = decimal * Decimal(1_000_000)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &scaled, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }

    public static func usdMicros(forFraction fraction: Double, maxUsdMicros: Int64) -> Int64 {
        guard maxUsdMicros > 0 else { return 0 }
        let clamped = min(1, max(0, fraction))
        if clamped >= 1 { return maxUsdMicros }
        let product = Decimal(maxUsdMicros) * Decimal(clamped)
        var rounded = Decimal()
        var source = product
        NSDecimalRound(&rounded, &source, 0, .plain)
        return (rounded as NSDecimalNumber).int64Value
    }

    public static func fraction(forUsdMicros usdMicros: Int64, maxUsdMicros: Int64) -> Double {
        guard maxUsdMicros > 0 else { return 0 }
        let clamped = min(maxUsdMicros, max(0, usdMicros))
        return Double(clamped) / Double(maxUsdMicros)
    }

    public static func shareMicros(
        forUsdMicros usdMicros: Int64,
        totalEquityUsdMicros: Int64,
        maxShareMicros: Int64
    ) -> Int64? {
        guard usdMicros > 0, totalEquityUsdMicros > 0, maxShareMicros > 0 else { return nil }
        if usdMicros >= totalEquityUsdMicros { return maxShareMicros }
        let product = Decimal(usdMicros) * Decimal(maxShareMicros)
        let quotient = product / Decimal(totalEquityUsdMicros)
        var rounded = Decimal()
        var source = quotient
        NSDecimalRound(&rounded, &source, 0, .plain)
        let share = (rounded as NSDecimalNumber).int64Value
        return share > 0 ? share : nil
    }

    public static func isFullWithdraw(selectedUsdMicros: Int64, totalEquityUsdMicros: Int64) -> Bool {
        selectedUsdMicros >= totalEquityUsdMicros
    }
}

public enum CatalogAssetNameFormatter {
    /// User-facing catalog name without trailing xStocks branding (e.g. "Apple xStock" → "Apple").
    public static func format(_ catalogName: String) -> String {
        var name = catalogName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else { return name }

        let suffixes = [" xStock", " xStocks", " xstock", " xstocks"]
        for suffix in suffixes {
            if name.lowercased().hasSuffix(suffix.lowercased()) {
                name = String(name.dropLast(suffix.count))
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                break
            }
        }
        return name
    }
}
