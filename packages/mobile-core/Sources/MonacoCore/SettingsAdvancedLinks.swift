import Foundation

public struct SettingsExplorerLinkDTO: Equatable {
    public let id: String
    public let title: String
    public let urlString: String

    public init(id: String, title: String, urlString: String) {
        self.id = id
        self.title = title
        self.urlString = urlString
    }
}

public enum SettingsAdvancedCatalog {
    public static let explorerLinks: [SettingsExplorerLinkDTO] = [
        SettingsExplorerLinkDTO(id: "solscan", title: "Solscan explorer", urlString: "https://solscan.io"),
        SettingsExplorerLinkDTO(id: "solana-fm", title: "Solana FM explorer", urlString: "https://solana.fm"),
    ]

    public static func containsExplorerLinksOnly() -> Bool {
        let allowedHosts = Set(["solscan.io", "solana.fm", "www.solscan.io"])
        return explorerLinks.allSatisfy { link in
            guard let host = URL(string: link.urlString)?.host?.lowercased() else { return false }
            return allowedHosts.contains(host)
        }
    }
}
