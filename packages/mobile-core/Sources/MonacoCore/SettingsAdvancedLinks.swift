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
        SettingsExplorerLinkDTO(id: "basescan-address", title: "View on Basescan", urlString: "https://basescan.org/address/"),
        SettingsExplorerLinkDTO(id: "basescan-tx", title: "View on Basescan", urlString: "https://basescan.org/tx/"),
    ]

    public static func containsExplorerLinksOnly() -> Bool {
        let allowedHosts = Set(["basescan.org", "www.basescan.org"])
        return explorerLinks.allSatisfy { link in
            guard let host = URL(string: link.urlString)?.host?.lowercased() else { return false }
            return allowedHosts.contains(host)
        }
    }
}
