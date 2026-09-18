import MonacoCore
import SwiftUI

/// Opens backend session then loads app home for signed-in users.
struct SessionGateView: View {
    @ObservedObject var auth: PrivyAuthService

    private let apiClient = MonacoAPIClient()

    @StateObject private var sharedState = MonacoSessionState()
    private var home: HomeViewDTO? { sharedState.value?.home }
    @State private var profile: MeResponse?
    @State private var showOnboarding = false
    @State private var errorMessage: String?
    @State private var refreshToast: MonacoToast?
    @State private var isLoading = true
    @State private var sessionStore = MonacoSessionStore()

    var body: some View {
        Group {
            if isLoading {
                ProgressView("Loading your boards…")
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, minHeight: 200)
            } else if showOnboarding, let profile {
                OnboardingFlowView(auth: auth, initialProfile: profile) {
                    await completeOnboarding()
                }
            } else if let home, let profile {
                MainTabView(auth: auth, home: home, profile: sharedState.value?.profile ?? profile, onRefresh: refreshHome)
                    .environment(\.monacoSessionSnapshot, sharedState.value)
                    .environment(\.monacoSessionRevision, sharedState.revision)
                    .environment(\.refreshMonacoSession, refreshHome)
            } else if let errorMessage {
                VStack(alignment: .leading, spacing: 12) {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote)
                        .foregroundStyle(MonacoTheme.destructive)

                    Button("Try again") {
                        Task { await openSessionAndLoadHome() }
                    }
                    .buttonStyle(.monacoPrimary)
                }
                .padding()
                .monacoSurfaceCard()
                .padding()
            }
        }
        .monacoToast($refreshToast)
        .onReceive(NotificationCenter.default.publisher(for: .monacoSessionChanged)) { _ in
            Task { await refreshHome() }
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
            sharedState.clear()
            profile = nil
            showOnboarding = false
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        sharedState.clear()
        profile = nil
        showOnboarding = false

        do {
            let session = try await apiClient.openSession(accessToken: accessToken)
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            if auth.shouldInvalidateBackendSession(serverUserId: session.userId) {
                await auth.logout()
                return
            }
            auth.recordBackendSession(userId: session.userId)

            let me = try await apiClient.me(accessToken: accessToken)
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            profile = me

            if needsOnboarding(profile: me) {
                showOnboarding = true
                isLoading = false
                return
            }

            await loadHome(accessToken: accessToken)
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            errorMessage = "Could not open session (HTTP \(status))."
            isLoading = false
        } catch {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            errorMessage = "Could not connect to Monaco."
            isLoading = false
        }
    }

    private func completeOnboarding() async {
        showOnboarding = false
        isLoading = true

        guard let accessToken = auth.accessToken else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        do {
            let me = try await apiClient.me(accessToken: accessToken)
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            profile = me
            await loadHome(accessToken: accessToken)
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            errorMessage = "Could not refresh profile (HTTP \(status))."
            isLoading = false
        } catch {
            guard auth.accessToken == accessToken, !Task.isCancelled else { return }
            errorMessage = "Could not connect to Monaco."
            isLoading = false
        }
    }

    private func needsOnboarding(profile: MeResponse) -> Bool {
        let trimmedName = profile.displayName.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmedName.isEmpty {
            return true
        }
        return !sessionStore.isOnboardingCompleted(for: profile.userId)
    }

    private func loadHome(accessToken: String? = nil) async {
        let token = accessToken ?? auth.accessToken
        guard let token else {
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        do {
            try await sharedState.refresh {
                async let boards = apiClient.getHome(accessToken: token)
                async let me = apiClient.me(accessToken: token)
                let snapshot = try await MonacoSessionSnapshot(home: boards, profile: me)
                guard auth.accessToken == token else { throw CancellationError() }
                return snapshot
            }
            guard auth.accessToken == token, !Task.isCancelled else { return }
            profile = sharedState.value?.profile ?? profile
            errorMessage = nil
        } catch MonacoAPIError.httpStatus(let status) where status == 401 {
            guard auth.accessToken == token, !Task.isCancelled else { return }
            await auth.logout()
        } catch MonacoAPIError.httpStatus(let status) {
            guard auth.accessToken == token, !Task.isCancelled else { return }
            errorMessage = "Could not refresh your cabals (HTTP \(status))."
        } catch {
            guard auth.accessToken == token, !Task.isCancelled else { return }
            errorMessage = "Could not refresh your cabals."
        }

        if sharedState.value != nil, let errorMessage {
            refreshToast = MonacoToast(message: errorMessage + " Pull down to retry.")
        }
        isLoading = false
    }
}

#Preview {
    SessionGateView(auth: PrivyAuthService())
}
