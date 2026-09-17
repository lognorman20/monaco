import Foundation

public enum PercentReturnFormatter {
    public static func format(_ raw: String?) -> String {
        guard let raw, !raw.isEmpty else { return "—" }
        if raw.hasPrefix("+") || raw.hasPrefix("-") { return raw }
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
