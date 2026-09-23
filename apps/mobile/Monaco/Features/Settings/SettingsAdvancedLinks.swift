import Foundation

struct SettingsExplorerLink: Identifiable, Equatable {
    let id: String
    let title: String
    let url: URL
}

enum SettingsAdvancedLinks {
    /// The link's destination without the scheme, as a row subtitle. Both explorer links carry the
    /// same title, so without the path the list is two identical rows.
    static func displayURL(_ url: URL) -> String? {
        guard let host = url.host() else { return nil }
        let path = url.path()
        return path.isEmpty ? host : host + path
    }

    static let explorerLinks: [SettingsExplorerLink] = [
        SettingsExplorerLink(
            id: "basescan-address",
            title: "View on Basescan",
            url: URL(string: "https://basescan.org/address/")!
        ),
        SettingsExplorerLink(
            id: "basescan-tx",
            title: "View on Basescan",
            url: URL(string: "https://basescan.org/tx/")!
        ),
    ]
}
