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
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 24) {
                    HStack {
                        MonacoIdentityMark(title: displayName, size: 76)
                        Spacer()
                        Text("MONACO").font(.caption.weight(.bold)).tracking(3)
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        Text(displayName)
                            .font(MonacoTheme.display(36))
                            .fixedSize(horizontal: false, vertical: true)
                            .accessibilityAddTraits(.isHeader)
                        Text("Your investing profile")
                            .font(.subheadline).foregroundStyle(MonacoTheme.secondaryText)
                    }
                }
                .padding(24)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(LinearGradient(colors: [MonacoTheme.mint, MonacoTheme.butter.opacity(0.6)], startPoint: .topLeading, endPoint: .bottomTrailing), in: RoundedRectangle(cornerRadius: 28))

                NavigationLink {
                    UserProfileGroupsView(auth: auth, userId: profile.userId, displayName: profile.displayName)
                } label: {
                    HStack(spacing: 16) {
                        Image(systemName: "person.2").font(.title3)
                            .frame(width: 48, height: 48)
                            .background(MonacoTheme.mint, in: Circle())
                        Text("Your cabals").font(MonacoTheme.display(20))
                        Spacer()
                        Image(systemName: "arrow.up.right")
                    }
                    .padding(20)
                    .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 24))
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("profile-your-cabals-link")

                VStack(alignment: .leading, spacing: 16) {
                    Label("Deposit address", systemImage: "arrow.down.left")
                        .font(MonacoTheme.display(20))
                    MonacoWalletAddressText(address: profile.memberWalletAddress)
                        .padding(16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(MonacoTheme.background, in: RoundedRectangle(cornerRadius: 16))
                    Text("To add money, open your cabal and choose Add money.")
                        .font(.subheadline).foregroundStyle(MonacoTheme.secondaryText)
                }
                .padding(20)
                .background(MonacoTheme.surface, in: RoundedRectangle(cornerRadius: 24))
            }
            .padding(20)
        }
        .foregroundStyle(MonacoTheme.primaryText)
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
