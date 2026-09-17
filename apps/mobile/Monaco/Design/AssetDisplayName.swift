import Foundation

/// Strips xStocks catalog branding from user-facing asset names.
enum AssetDisplayName {
    static func format(catalogName: String) -> String {
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

extension CatalogAssetDTO {
    var displayName: String {
        AssetDisplayName.format(catalogName: name)
    }
}
