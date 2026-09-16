import SwiftUI

/// App settings — explorer links live under Advanced only.
struct SettingsView: View {
    @ObservedObject var auth: PrivyAuthService
    let memberWalletAddress: String?
    let treasuryAddress: String?

    var body: some View {
        Form {
            Section {
                NavigationLink {
                    AdvancedSettingsView(
                        memberWalletAddress: memberWalletAddress,
                        treasuryAddress: treasuryAddress
                    )
                } label: {
                    Label("Advanced", systemImage: "link")
                }
                .accessibilityIdentifier("settings-advanced-link")
            }

            Section {
                Button("Sign out") {
                    Task { await auth.logout() }
                }
            }
        }
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
    }
}

struct AdvancedSettingsView: View {
    let memberWalletAddress: String?
    let treasuryAddress: String?

    var body: some View {
        Form {
            Section("Block explorers") {
                ForEach(SettingsAdvancedLinks.explorerLinks) { link in
                    Link(destination: link.url) {
                        Label(link.title, systemImage: "safari")
                    }
                    .accessibilityIdentifier("settings-explorer-\(link.id)")
                }
            }

            if memberWalletAddress != nil || treasuryAddress != nil {
                Section("On-chain addresses") {
                    if let memberWalletAddress {
                        addressRow(title: "Your deposit address", value: memberWalletAddress)
                    }
                    if let treasuryAddress {
                        addressRow(title: "Club treasury", value: treasuryAddress)
                    }
                }
            }
        }
        .navigationTitle("Advanced")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func addressRow(title: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.body.monospaced())
                .textSelection(.enabled)
        }
    }
}

#Preview {
    NavigationStack {
        SettingsView(
            auth: PrivyAuthService(),
            memberWalletAddress: "DemoMemberAddress1111111111111111111111",
            treasuryAddress: "DemoTreasuryAddress2222222222222222222222"
        )
    }
}
