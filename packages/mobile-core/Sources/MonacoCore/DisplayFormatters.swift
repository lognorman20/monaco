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
