import SwiftUI

/// Signed-in profile MVP. Avatar upload is #160.
struct ProfileTabView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    private var displayName: String {
        let name = session.me?.displayName.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? "Member" : name
    }

    private var depositAddress: String? {
        let address = session.me?.memberWalletAddress.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !address.isEmpty, !address.hasPrefix("FAKE") else { return nil }
        return address
    }

    var body: some View {
        MonacoScreen {
            profileScroll
        }
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await session.refresh(auth: auth)
        }
        .accessibilityIdentifier("profile-root")
    }

    private var profileScroll: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                MonacoHeroHeader(title: displayName, caption: "Your profile")

                if let depositAddress {
                    MonacoCard {
                        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                            Text("Deposit address")
                                .font(MonacoTheme.TypeRole.caption)
                                .foregroundStyle(MonacoTheme.muted)
                            MonacoWalletAddressText(address: depositAddress)
                                .accessibilityIdentifier("profile-deposit-address")
                            Text("Send USDC on Solana here to add to your account balance.")
                                .monacoSecondaryCaption()
                        }
                    }
                }

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    Text("Your cabals")
                        .font(MonacoTheme.TypeRole.title)
                        .foregroundStyle(MonacoTheme.ink)

                    if session.joinedCabals.isEmpty {
                        MonacoEmptyStateCard(
                            message: "Join a cabal to see it here.",
                            systemImage: "person.3"
                        )
                    } else {
                        ForEach(session.joinedCabals) { row in
                            NavigationLink {
                                GroupDetailView(
                                    auth: auth,
                                    groupId: row.groupId,
                                    groupName: row.name,
                                    onLeft: { await session.refresh(auth: auth) }
                                )
                            } label: {
                                MonacoRowCard(
                                    systemImage: "person.3.fill",
                                    title: row.name,
                                    subtitle: nil,
                                    trailing: "$\(row.potValueUsd)"
                                )
                            }
                            .buttonStyle(.plain)
                            .accessibilityIdentifier("profile-cabal-\(row.groupId)")
                        }
                    }
                }
            }
            .padding(MonacoTheme.Space.m)
        }
    }
}
