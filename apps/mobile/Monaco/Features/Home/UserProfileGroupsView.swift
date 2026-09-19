import MonacoCore
import SwiftUI

/// Profile list of clubs shared with a people-board row member.
struct UserProfileGroupsView: View {
    @ObservedObject var auth: PrivyAuthService
    let userId: String
    let displayName: String

    private let apiClient = MonacoAPIClient()

    @State private var groups: [HomeGroupBoardRowDTO] = []
    @State private var isLoading = true
    @State private var errorMessage: String?

    var body: some View {
        List {
            Section("\(displayName)'s clubs") {
                if isLoading {
                    ProgressView("Loading clubs…")
                        .foregroundStyle(MonacoTheme.secondaryText)
                } else if let errorMessage {
                    MonacoEmptyStateCard(
                        message: errorMessage,
                        systemImage: "exclamationmark.triangle"
                    )
                } else if groups.isEmpty {
                    MonacoEmptyStateCard(
                        message: "No shared clubs yet.",
                        systemImage: "person.3"
                    )
                } else {
                    ForEach(groups) { row in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.name)
                                .font(.body.bold())
                                .foregroundStyle(MonacoTheme.primaryText)
                            Text(row.dollarPnl)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(MonacoTheme.secondaryText)
                        }
                    }
                }
            }
        }
        .monacoInsetList()
        .background(MonacoTheme.background)
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: userId) {
            await loadSharedGroups()
        }
    }

    private func loadSharedGroups() async {
        guard let accessToken = auth.accessToken else {
            groups = []
            errorMessage = "Missing sign-in token."
            isLoading = false
            return
        }

        isLoading = true
        errorMessage = nil
        defer { isLoading = false }

        do {
            groups = try await apiClient.getUserSharedGroups(accessToken: accessToken, userId: userId)
        } catch MonacoAPIError.httpStatus(let status) {
            groups = []
            errorMessage = "Could not load clubs (HTTP \(status))."
        } catch {
            groups = []
            errorMessage = "Could not load shared clubs."
        }
    }
}
