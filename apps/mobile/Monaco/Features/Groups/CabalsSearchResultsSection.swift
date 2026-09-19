import MonacoCore
import SwiftUI

/// Search results replace the tab content while a query is typed.
struct CabalsSearchResultsSection: View {
    @ObservedObject var auth: PrivyAuthService
    let model: CabalsTabModel
    var onChanged: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            switch model.searchState {
            case .idle:
                EmptyView()
            case .tooShort:
                hint("Type at least \(GroupSearchQuery.minimumLength) letters.", id: "cabals-search-too-short")
            case .loading:
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .accessibilityIdentifier("cabals-search-loading")
            case .empty:
                EmptyState(
                    title: "No cabal called \u{201C}\(model.query.trimmingCharacters(in: .whitespacesAndNewlines))\u{201D}."
                )
                .accessibilityIdentifier("cabals-search-empty")
            case .failed:
                EmptyState(
                    title: "Search didn't go through",
                    actionTitle: "Try again",
                    action: { model.retrySearch() }
                )
                .accessibilityIdentifier("cabals-search-error")
            case .results:
                results
            }
        }
    }

    private var results: some View {
        LazyVStack(spacing: MonacoTheme.Space.s) {
            MonacoGroupedList {
                ForEach(Array(model.results.enumerated()), id: \.element.id) { index, row in
                    NavigationLink {
                        CabalDiscoveryDestinationView(
                            auth: auth,
                            groupId: row.groupID,
                            name: row.name,
                            destination: GroupDiscoveryDestination(isJoined: row.isJoined, joinMode: row.joinMode),
                            onChanged: onChanged
                        )
                    } label: {
                        CabalDiscoveryRowContent(
                            rank: nil,
                            groupId: row.groupID,
                            name: row.name,
                            detail: cabalRowDetail(memberCount: row.memberCount, isJoined: row.isJoined, joinMode: row.joinMode),
                            potValueUsd: row.potValueUsd,
                            percentReturn: row.percentReturn,
                            isLast: index == model.results.count - 1
                        )
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("cabals-search-result-\(row.groupID)")
                }
            }

            if model.nextCursor != nil {
                Button {
                    Task { await model.loadMoreResults() }
                } label: {
                    if model.isLoadingMore {
                        ProgressView().tint(MonacoTheme.accent)
                    } else {
                        Text("Show more cabals")
                    }
                }
                .buttonStyle(.monacoSecondary)
                .disabled(model.isLoadingMore)
                .accessibilityIdentifier("cabals-search-more")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("cabals-search-results")
    }

    private func hint(_ text: String, id: String) -> some View {
        Text(text)
            .monacoSecondaryCaption()
            .accessibilityIdentifier(id)
    }
}
