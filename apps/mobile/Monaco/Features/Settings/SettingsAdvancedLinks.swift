import Foundation

struct SettingsExplorerLink: Identifiable, Equatable {
    let id: String
    let title: String
    let url: URL
}

enum SettingsAdvancedLinks {
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
