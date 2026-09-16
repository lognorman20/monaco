import SwiftUI

/// Opens backend session then loads app home for signed-in users.
struct SessionGateView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @State private var home: HomeViewDTO?
    @State private var errorMessage: String?
    @State private var isLoading = true

    var body: some View {
        Group {
            if isLoading {
                ProgressView("Loading your boards…")
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else if let home {
                HomeView(auth: auth, home: home, onRefresh: refreshHome)
            } else if let errorMessage {
                VStack(alignment: .leading, spacing: 12) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(.orange)

                    Button("Try again") {
                        Task { await openSessionAndLoadHome() }
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
        }
        .task(id: auth.accessToken) {
            await openSessionAndLoadHome()
        }
    }

    private func refreshHome() async {
        await loadHome()
    }

    private func openSessionAndLoadHome() async {
        guard let accessToken = auth.accessToken else {
            home = nil
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        home = nil

        do {
            _ = try await apiClient.openSession(accessToken: accessToken)
            await loadHome(accessToken: accessToken)
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not open session (HTTP \(status))."
            isLoading = false
        } catch {
            errorMessage = "Could not connect to Monaco."
            isLoading = false
        }
    }

    private func loadHome(accessToken: String? = nil) async {
        let token = accessToken ?? auth.accessToken
        guard let token else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        do {
            home = try await apiClient.getHome(accessToken: token)
            errorMessage = nil
        } catch MonacoAPIError.httpStatus(let status) {
            errorMessage = "Could not load home (HTTP \(status))."
            home = nil
        } catch {
            errorMessage = "Could not load your boards."
            home = nil
        }

        isLoading = false
    }
}

#Preview {
    SessionGateView(auth: PrivyAuthService())
}
