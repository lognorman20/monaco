import SwiftUI

/// M1 post-login proof screen: opens backend session then loads GET /v1/me.
struct MeScreenView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var profile: MeResponse?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Text("Your account")
                .font(.title2.bold())

            if isLoading {
                ProgressView("Loading profile…")
            } else if let profile {
                profileSection(profile)
            } else if let errorMessage {
                Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.orange)
            }

            NavigationLink {
                CreateGroupView(auth: auth)
            } label: {
                Label("Create group", systemImage: "person.3.fill")
            }
            .buttonStyle(.borderedProminent)

            Button("Sign out") {
                Task { await auth.logout() }
            }
            .buttonStyle(.bordered)
        }
        .task(id: auth.accessToken) {
            await loadProfile()
        }
    }

    @ViewBuilder
    private func profileSection(_ profile: MeResponse) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            profileRow(title: "User ID", value: profile.userId)

            if !profile.displayName.isEmpty {
                profileRow(title: "Display name", value: profile.displayName)
            }

            VStack(alignment: .leading, spacing: 4) {
                Text("Member wallet")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(profile.memberWalletAddress)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
            }

            Label("Connected Solana address from GET /v1/me", systemImage: "checkmark.seal.fill")
                .font(.footnote)
                .foregroundStyle(.green)
        }
    }

    private func profileRow(title: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.body)
                .textSelection(.enabled)
        }
    }

    private func loadProfile() async {
        guard let accessToken = auth.accessToken else {
            profile = nil
            errorMessage = "Missing Privy access token."
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        profile = nil

        do {
            _ = try await apiClient.openSession(accessToken: accessToken)
            profile = try await apiClient.me(accessToken: accessToken)
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "API error (\(status)). Is the backend running?"
        } catch {
            errorMessage = "Could not load profile from API."
        }

        isLoading = false
    }
}

#Preview {
    MeScreenView(auth: PrivyAuthService())
}
