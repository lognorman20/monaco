import Foundation

struct SettingsExplorerLink: Identifiable, Equatable {
    let id: String
    let title: String
    let url: URL
}

enum SettingsAdvancedLinks {
    static let explorerLinks: [SettingsExplorerLink] = [
        SettingsExplorerLink(
            id: "solscan",
            title: "Solscan explorer",
            url: URL(string: "https://solscan.io")!
        ),
        SettingsExplorerLink(
            id: "solana-fm",
            title: "Solana FM explorer",
            url: URL(string: "https://solana.fm")!
        ),
    ]
}
