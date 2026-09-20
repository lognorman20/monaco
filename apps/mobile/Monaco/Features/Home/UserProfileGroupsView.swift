import MonacoCore
import SwiftUI

/// Where a "Top investors" row leads: the cabals the viewer shares with that member.
///
/// The endpoint only ever returns shared cabals, so the screen says so — it used to be
/// headed "<name>'s cabals" over a list that was empty for everyone the viewer had not
/// joined a cabal with (#280). Each row is the shared cabal itself and opens it, the pot's
/// P&L is formatted the way it is everywhere else, and the error state has the retry its
/// copy promises.
struct UserProfileGroupsView: View {
    @ObservedObject var auth: PrivyAuthService
    let userId: String
    let displayName: String
    var profilePhotoUrl: String? = nil

    private let apiClient = MonacoAPIClient()

    /// Why the list is not on screen. `signedOut` has no retry: trying again cannot help.
    private enum LoadFailure {
        case couldNotLoad
        case signedOut
    }

    @State private var groups: [HomeGroupBoardRowDTO] = []
    @State private var isLoading = true
    @State private var failure: LoadFailure?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                header

                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    MonacoSectionHeader("Cabals you share")
                    content
                }
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            .padding(.vertical, MonacoTheme.Space.m)
        }
        .scrollBounceBehavior(.always)
        .monacoCanvas()
        .navigationTitle(displayName)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable {
            await loadSharedGroups()
        }
        .task(id: userId) {
            await loadSharedGroups()
        }
    }

    private var header: some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            MonacoAvatar(photoURL: profilePhotoUrl, displayName: displayName, size: 44)
            Text(displayName)
                .font(MonacoTheme.Typo.section)
                .foregroundStyle(MonacoTheme.ink)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("user-profile-header")
    }

    @ViewBuilder
    private var content: some View {
        if isLoading, groups.isEmpty {
            VStack(spacing: MonacoTheme.Space.s) {
                ForEach(0..<2, id: \.self) { _ in
                    SkeletonBlock(height: 60, radius: MonacoTheme.Radius.card)
                }
            }
            .accessibilityIdentifier("user-profile-groups-loading")
        } else if let failure {
            switch failure {
            case .couldNotLoad:
                EmptyState(
                    title: "Couldn't load this",
                    message: "Check your connection and try again.",
                    actionTitle: "Try again",
                    action: { Task { await loadSharedGroups() } }
                )
                .accessibilityIdentifier("user-profile-groups-error")
            case .signedOut:
                EmptyState(
                    title: "You're signed out",
                    message: "Sign in again to see the cabals you share."
                )
                .accessibilityIdentifier("user-profile-groups-signed-out")
            }
        } else if groups.isEmpty {
            EmptyState(
                title: "No cabals in common",
                message: "You and \(displayName) aren't in a cabal together yet."
            )
            .accessibilityIdentifier("user-profile-groups-empty")
        } else {
            MonacoGroupedList {
                ForEach(groups) { row in
                    NavigationLink {
                        GroupDetailView(auth: auth, groupId: row.groupId, groupName: row.name)
                    } label: {
                        MonacoRow(
                            // The figures are the pot's, not this member's slice, so the row
                            // says whose they are.
                            title: row.name,
                            subtitle: CabalPositionRowFigures.potSubtitle(potValueUsd: row.potValueUsd),
                            chevron: true,
                            isLast: row.groupId == groups.last?.groupId,
                            leading: { CabalMark(groupId: row.groupId, name: row.name) },
                            trailing: {
                                PercentText(percentReturn: row.percentReturn, style: .row)
                                PnLText(dollarPnl: row.dollarPnl, style: .caption)
                            }
                        )
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("user-profile-group-\(row.groupId)")
                }
            }
        }
    }

    private func loadSharedGroups() async {
        guard let accessToken = auth.accessToken else {
            groups = []
            failure = .signedOut
            isLoading = false
            return
        }

        isLoading = true
        failure = nil
        defer { isLoading = false }

        do {
            groups = try await apiClient.getUserSharedGroups(accessToken: accessToken, userId: userId)
        } catch {
            // Leaving the screen cancels the read; that is not a failure to report.
            if error.isRequestCancellation { return }
            if case MonacoAPIError.httpStatus(let status) = error, status == 401 {
                await auth.signOutAfterRejectedSession()
                return
            }
            groups = []
            failure = .couldNotLoad
        }
    }
}
