import MonacoCore
import SwiftUI

/// "Challenge a cabal": the cabal search, and a Challenge button on every result. A sent challenge
/// is confirmed with a toast and the row says it was sent; the other cabal answers from its own
/// matchup screen.
struct ChallengeCabalView: View {
    let groupId: String
    let groupName: String
    let source: any MatchupDataSource

    @State private var search: CabalsTabModel
    @State private var sender: ChallengeSendModel
    @State private var query = ""
    @State private var toast: MonacoToast?

    init(
        groupId: String,
        groupName: String,
        source: any MatchupDataSource,
        alreadyChallenged: Set<String> = [],
        initialQuery: String = ""
    ) {
        self.groupId = groupId
        self.groupName = groupName
        self.source = source
        _search = State(initialValue: CabalsTabModel(dataSource: MatchupCabalSearchSource(source: source)))
        _sender = State(initialValue: ChallengeSendModel(groupId: groupId, alreadyChallenged: alreadyChallenged))
        _query = State(initialValue: initialQuery)
    }

    /// The results a cabal can challenge: never itself.
    private var opponents: [GroupDiscoveryRowDTO] {
        search.results.filter { $0.groupID.lowercased() != groupId.lowercased() }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.l) {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.sm) {
                    Text(MatchupCopy.challengeHint)
                        .font(MonacoTheme.Typo.callout)
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .fixedSize(horizontal: false, vertical: true)
                    MonacoSearchField(placeholder: MatchupCopy.searchPrompt, text: $query)
                        .accessibilityIdentifier("challenge-search")
                }
                .padding(.horizontal, MonacoTheme.Space.m)

                results
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .monacoCanvas()
        .navigationTitle(MatchupCopy.challengeTitle)
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: query, initial: true) { _, newValue in
            search.updateQuery(newValue)
        }
        .monacoToast($toast)
        .accessibilityIdentifier("challenge-screen")
    }

    @ViewBuilder
    private var results: some View {
        switch search.searchState {
        case .idle:
            EmptyState(title: "Find a cabal to play", message: "Search by name. Your cabal will not show up.")
                .accessibilityIdentifier("challenge-idle")
        case .tooShort:
            Text("Type at least \(GroupSearchQuery.minimumLength) letters.")
                .font(MonacoTheme.Typo.caption)
                .foregroundStyle(MonacoTheme.secondaryText)
                .padding(.horizontal, MonacoTheme.Space.m)
        case .loading:
            BoardRowSkeleton(rows: 3)
                .accessibilityIdentifier("challenge-loading")
        case .empty:
            EmptyState(title: "No cabal called \u{201C}\(query.trimmingCharacters(in: .whitespacesAndNewlines))\u{201D}.")
                .accessibilityIdentifier("challenge-empty")
        case .failed:
            EmptyState(title: "Search didn't go through", actionTitle: MatchupCopy.tryAgain, action: { search.retrySearch() })
                .accessibilityIdentifier("challenge-error")
        case .results:
            if opponents.isEmpty {
                EmptyState(title: "That's your own cabal", message: "Search for another one to play.")
            } else {
                MonacoGroupedList {
                    ForEach(opponents) { row in
                        ChallengeResultRow(
                            row: row,
                            isSent: sender.sent.contains(row.groupID),
                            isSending: sender.sending.contains(row.groupID),
                            isLast: row.groupID == opponents.last?.groupID,
                            onChallenge: {
                                Task {
                                    if let result = await sender.send(to: row, from: source) {
                                        toast = result
                                    }
                                }
                            }
                        )
                    }
                }
                .accessibilityIdentifier("challenge-results")
            }
        }
    }
}

/// One cabal in the challenge search: mark, name, members, and the Challenge button (or "Sent").
private struct ChallengeResultRow: View {
    let row: GroupDiscoveryRowDTO
    let isSent: Bool
    let isSending: Bool
    let isLast: Bool
    let onChallenge: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: MonacoTheme.Space.s))
            : AnyLayout(HStackLayout(spacing: MonacoTheme.Space.sm))
        layout {
            HStack(spacing: MonacoTheme.Space.sm) {
                CabalMark(groupId: row.groupID, name: row.name, size: 44, pictureUrl: row.pictureUrl)
                VStack(alignment: .leading, spacing: 2) {
                    Text(row.name)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.ink)
                        .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
                    Text(row.memberCount == 1 ? "1 member" : "\(row.memberCount) members")
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            if isSent {
                Text(MatchupCopy.challengeSentLabel)
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.secondaryText)
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("challenge-sent-\(row.groupID)")
            } else {
                Button(action: onChallenge) {
                    Group {
                        if isSending {
                            ProgressView().tint(MonacoTheme.primaryButtonLabel)
                        } else {
                            Text(MatchupCopy.challengeAction)
                        }
                    }
                    .font(MonacoTheme.Typo.calloutStrong)
                    .foregroundStyle(MonacoTheme.primaryButtonLabel)
                    .padding(.horizontal, 14)
                    .frame(minHeight: 36)
                    .background(Capsule().fill(MonacoTheme.primaryButtonFill))
                    .frame(minHeight: 44)
                }
                .buttonStyle(.plain)
                .disabled(isSending)
                .accessibilityIdentifier("challenge-send-\(row.groupID)")
            }
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .padding(.vertical, 8)
        .frame(minHeight: 60)
        .overlay(alignment: .bottom) {
            if !isLast {
                MonacoRule().padding(.leading, MonacoTheme.Space.m + 44 + MonacoTheme.Space.sm)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
