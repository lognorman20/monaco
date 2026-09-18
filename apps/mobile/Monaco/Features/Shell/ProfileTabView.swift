import SwiftUI

/// Signed-in user profile — full profile screen deferred to a dedicated lane.
struct ProfileTabView: View {
    @ObservedObject var auth: PrivyAuthService
    let profile: MeResponse

    private var displayName: String {
        let trimmed = profile.displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? "Your profile" : trimmed
    }

    var body: some View {
        List {
            Section {
                if !profile.displayName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    Text(profile.displayName)
                        .font(.title3.bold())
                        .foregroundStyle(MonacoTheme.primaryText)
                }

                VStack(alignment: .leading, spacing: 4) {
                    Text("Deposit address")
                        .font(.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                    MonacoWalletAddressText(address: profile.memberWalletAddress)
                }
            }

            Section {
                NavigationLink {
                    UserProfileGroupsView(
                        auth: auth,
                        userId: profile.userId,
                        displayName: profile.displayName
                    )
                } label: {
                    Label("Your cabals", systemImage: "person.3")
                        .foregroundStyle(MonacoTheme.primaryText)
                }
                .accessibilityIdentifier("profile-your-cabals-link")
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.large)
    }
}

#Preview {
    NavigationStack {
        ProfileTabView(
            auth: PrivyAuthService(),
            profile: MeResponse(
                userId: "preview",
                displayName: "Alfred",
                memberWalletAddress: "DemoMemberAddress1111111111111111111111"
            )
        )
        .monacoRootAppearance()
    }
}
