import Foundation

/// Typographic minus sign (U+2212) used for every negative figure shown to people.
let typographicMinus = "\u{2212}"

public enum PercentReturnFormatter {
    /// Formats a backend return ratio ("0.124", "-0.036") as "+12.4%" / "−3.6%" (U+2212).
    /// Ratios that round to 0.0% render "0.0%" with no sign. Nil / empty renders "—".
    /// Strings that are already percentages ("+12.4%") pass through with the minus normalised.
    public static func format(_ raw: String?) -> String {
        guard let raw else { return "—" }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != "—" else { return "—" }
        if trimmed.hasSuffix("%") {
            if trimmed.hasPrefix("-") {
                return typographicMinus + trimmed.dropFirst()
            }
            return trimmed
        }
        guard let value = Double(trimmed.replacingOccurrences(of: typographicMinus, with: "-")),
              value.isFinite
        else {
            // "nan" and "inf" parse as Doubles. Neither is a return anyone can read.
            return "—"
        }
        let pct = value * 100
        let magnitude = String(format: "%.1f", abs(pct))
        if magnitude == "0.0" { return "0.0%" }
        return (pct < 0 ? typographicMinus : "+") + magnitude + "%"
    }
}

public enum DollarPnlFormatter {
    public static func format(_ raw: String) -> String {
        if raw.hasPrefix("-") || raw.hasPrefix(typographicMinus) {
            return "\(raw) loss"
        }
        return raw
    }
}

public enum SlicePercentFormatter {
    /// A NaN or infinite ratio reads as no figure, the same as `PercentReturnFormatter`.
    /// Returning the raw string just moved the garbage: the member read "nan" in place of
    /// "nan%".
    public static func format(_ raw: String) -> String {
        guard let value = Double(raw), value.isFinite else { return "—" }
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

    /// Ticker as people know it: "AAPLx" → "AAPL", "BRK.Bx" → "BRK.B". "USDC" stays; a mint → "Unknown stock".
    /// Keep the raw symbol for API calls.
    public static func display(_ symbol: String) -> String {
        let formatted = format(symbol)
        guard formatted.count >= 2, formatted.count <= 7, formatted.last == "x" else { return formatted }
        let body = formatted.dropLast()
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZ.")
        guard body.unicodeScalars.allSatisfy({ allowed.contains($0) }) else { return formatted }
        return String(body)
    }

    private static func looksLikeSolanaMint(_ value: String) -> Bool {
        guard (32...44).contains(value.count) else { return false }
        let base58 = CharacterSet(charactersIn: "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")
        return value.unicodeScalars.allSatisfy { base58.contains($0) }
    }
}

/// Company names for the xStocks catalog, for surfaces whose DTO only carries a symbol.
/// Follow-up: the API returns `assetName` on proposal rows and this table goes away.
public enum AssetDisplayNames {
    private static let names: [String: String] = [
        "AAPL": "Apple",
        "ABBV": "AbbVie",
        "ABT": "Abbott",
        "ACN": "Accenture",
        "AMBR": "Amber",
        "AMZN": "Amazon",
        "APP": "AppLovin",
        "AVGO": "Broadcom",
        "AZN": "AstraZeneca",
        "BAC": "Bank of America",
        "BRK.B": "Berkshire Hathaway",
        "CMCSA": "Comcast",
        "COIN": "Coinbase",
        "CRCL": "Circle",
        "CRM": "Salesforce",
        "CRWD": "CrowdStrike",
        "CSCO": "Cisco",
        "CVX": "Chevron",
        "DHR": "Danaher",
        "GLD": "Gold",
        "GME": "GameStop",
        "GOOGL": "Alphabet",
        "GS": "Goldman Sachs",
        "HD": "Home Depot",
        "HOOD": "Robinhood",
        "IBM": "IBM",
        "INTC": "Intel",
        "JNJ": "Johnson & Johnson",
        "JPM": "JPMorgan",
        "KO": "Coca-Cola",
        "LIN": "Linde",
        "LLY": "Eli Lilly",
        "MA": "Mastercard",
        "MCD": "McDonald's",
        "MDT": "Medtronic",
        "META": "Meta",
        "MRK": "Merck",
        "MRVL": "Marvell",
        "MSFT": "Microsoft",
        "MSTR": "Strategy",
        "NFLX": "Netflix",
        "NVDA": "Nvidia",
        "NVO": "Novo Nordisk",
        "ORCL": "Oracle",
        "PEP": "PepsiCo",
        "PFE": "Pfizer",
        "PG": "Procter & Gamble",
        "PLTR": "Palantir",
        "PM": "Philip Morris",
        "QQQ": "Nasdaq 100",
        "SPY": "S&P 500",
        "TBLL": "Treasury bills",
        "TMO": "Thermo Fisher",
        "TQQQ": "Nasdaq 100 3x",
        "TSLA": "Tesla",
        "UNH": "UnitedHealth",
        "V": "Visa",
        "VTI": "US total market",
        "WMT": "Walmart",
        "XOM": "Exxon Mobil",
    ]

    /// "AAPLx" or "AAPL" → "Apple". Nil when the symbol is not in the table.
    public static func name(forSymbol symbol: String) -> String? {
        let ticker = AssetSymbolFormatter.display(symbol).uppercased()
        return names[ticker]
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
        let isNegative = rounded < 0
        let magnitude = isNegative ? -rounded : rounded
        let number = magnitude as NSDecimalNumber
        let body = twoDecimalFormatter.string(from: number) ?? number.stringValue
        // A negative amount reads "−$12.50", the way `compact` already writes one: the sign
        // goes in front of the dollar sign, and it is a typographic minus, never "$-12.50".
        guard isNegative, body != "0.00" else { return "$\(body)" }
        return "\(typographicMinus)$\(body)"
    }

    /// Built once: this runs for every money label on every body pass.
    private static let twoDecimalFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.minimumFractionDigits = 2
        formatter.maximumFractionDigits = 2
        formatter.groupingSeparator = ","
        formatter.usesGroupingSeparator = true
        formatter.locale = posix
        return formatter
    }()
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

extension UsdAmountFormatter {
    /// "$124.5K" / "$1.2M" at ≥ $100,000 (strip cards); below that, the full "$12,431.80".
    public static func compact(decimalString: String) -> String {
        let posix = Locale(identifier: "en_US_POSIX")
        guard let decimal = Decimal(string: decimalString.trimmingCharacters(in: .whitespaces), locale: posix) else {
            return format(decimalString: decimalString)
        }
        let value = (decimal as NSDecimalNumber).doubleValue
        let magnitude = abs(value)
        guard magnitude >= 100_000 else { return format(decimal: decimal) }
        let sign = value < 0 ? typographicMinus : ""
        let (scaled, suffix): (Double, String) = magnitude >= 1_000_000_000
            ? (magnitude / 1_000_000_000, "B")
            : magnitude >= 1_000_000 ? (magnitude / 1_000_000, "M") : (magnitude / 1_000, "K")
        var body = String(format: "%.1f", scaled)
        if body.hasSuffix(".0") { body.removeLast(2) }
        return "\(sign)$\(body)\(suffix)"
    }
}

extension ProposalShareFormatter {
    /// "1.2034 shares", "0.5 shares", "1 share" from 8-decimal atomics. At most 4 decimals, trailing zeros trimmed.
    /// Dust below 0.0001 reads "< 0.0001 shares" rather than "0 shares".
    public static func sharesLabel(fromAtomics raw: String) -> String {
        guard let atomics = Decimal(string: raw.trimmingCharacters(in: .whitespaces), locale: Locale(identifier: "en_US_POSIX")),
              atomics >= 0 else { return raw }
        var shares = atomics / Decimal(sign: .plus, exponent: decimals, significand: 1)
        var rounded = Decimal()
        NSDecimalRound(&rounded, &shares, 4, .plain)
        if rounded == 0, shares > 0 { return "< 0.0001 shares" }
        let body = sharesLabelFormatter.string(from: rounded as NSDecimalNumber) ?? "\(rounded)"
        return rounded == 1 ? "1 share" : "\(body) shares"
    }

    private static let sharesLabelFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = true
        formatter.groupingSeparator = ","
        formatter.minimumFractionDigits = 0
        formatter.maximumFractionDigits = 4
        return formatter
    }()
}

/// Compact age from an ISO-8601 UTC timestamp: "now", "15m", "3h", then "Sep 14" (local calendar).
public enum RelativeTimeFormatter {
    public static func label(iso: String, now: Date = Date()) -> String {
        label(iso: iso, now: now, calendar: .current)
    }

    public static func label(iso: String, now: Date, calendar: Calendar) -> String {
        guard let date = parse(iso) else { return "" }
        let elapsed = now.timeIntervalSince(date)
        if elapsed < 60 { return "now" }
        if elapsed < 3600 { return "\(Int(elapsed / 60))m" }
        if elapsed < 86_400 { return "\(Int(elapsed / 3600))h" }
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        return SharedFormatters.string(
            from: date,
            pattern: .fixed(sameYear ? "MMM d" : "MMM d, yyyy"),
            locale: Locale(identifier: "en_US_POSIX"),
            calendar: calendar
        )
    }

    static func parse(_ raw: String) -> Date? {
        let trimmed = raw.trimmingCharacters(in: .whitespaces)
        return SharedFormatters.iso8601Date(from: trimmed)
    }
}
