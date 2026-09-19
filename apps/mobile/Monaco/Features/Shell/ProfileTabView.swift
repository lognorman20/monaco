import SwiftUI

/// Account identity, cabals, and personal deposit address.
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
                Text(displayName)
                    .font(.largeTitle.bold())
                    .foregroundStyle(MonacoTheme.primaryText)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                    .padding(.vertical, 12)
            }
            .listRowBackground(Color.clear)

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
                        .frame(minHeight: 44)
                }
                .accessibilityIdentifier("profile-your-cabals-link")
            }
            Section {
                MonacoWalletAddressText(address: profile.memberWalletAddress)
                    .padding(.vertical, 8)
            } header: {
                Text("Deposit address")
            } footer: {
                Text("To add money to a cabal, open that cabal and choose Add money.")
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.inline)
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
