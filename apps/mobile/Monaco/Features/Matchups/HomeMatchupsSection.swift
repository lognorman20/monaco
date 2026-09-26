import MonacoCore
import SwiftUI

/// Home's "This week": one card per matchup the member's cabals are in, right under the money.
/// The card is the thing they act on — it opens the cabal's matchup — so it is a card, on white.
///
/// Home includes this section only when the member has a cabal; with none, "Your cabals" already
/// says what to do next.
struct HomeMatchupsSection: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(\.matchupDataSource) private var injectedSource
    @State private var model = HomeMatchupsModel()

    private var source: any MatchupDataSource {
        injectedSource ?? LiveMatchupDataSource(auth: auth)
    }

    var body: some View {
        content
            .task { try? await model.load(from: source) }
            .pollWhileVisible(every: LiveRefreshCadence.resting, isActive: model.state != .hidden) {
                try await model.load(from: source, quiet: true)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .hidden:
            EmptyView()
        case .loading:
            section { card { MatchupScoreboardSkeleton() } }
                .accessibilityIdentifier("home-matchups-loading")
        case .failed:
            section {
                EmptyState(
                    title: "Couldn't load this week's matchups",
                    actionTitle: MatchupCopy.tryAgain,
                    action: { Task { await model.retry(from: source) } }
                )
            }
            .accessibilityIdentifier("home-matchups-error")
        case .loaded(let week):
            if !week.hasCabals {
                EmptyView()
            } else if !week.drawn {
                section {
                    EmptyState(title: MatchupCopy.drawPendingTitle, message: MatchupCopy.drawPendingDetail)
                }
                .accessibilityIdentifier("home-matchups-draw-pending")
            } else if week.matchups.isEmpty {
                section {
                    EmptyState(title: MatchupCopy.homeEmptyTitle, message: MatchupCopy.notInDrawDetail)
                }
                .accessibilityIdentifier("home-matchups-empty")
            } else {
                section {
                    VStack(spacing: MonacoTheme.Space.s) {
                        ForEach(week.matchups) { matchup in
                            NavigationLink {
                                MatchupView(
                                    auth: auth,
                                    groupId: matchup.a.groupID,
                                    groupName: matchup.a.name,
                                    source: source
                                )
                            } label: {
                                card {
                                    MatchupScoreboard(matchup: matchup, showsChevron: true)
                                }
                            }
                            .buttonStyle(MatchupCardButtonStyle())
                            .accessibilityHint("Opens the matchup")
                            .accessibilityIdentifier("home-matchup-\(matchup.a.groupID)")
                        }
                    }
                }
                .accessibilityIdentifier("home-matchups")
            }
        }
    }

    private func section<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader(MatchupCopy.homeSectionTitle)
                .padding(.horizontal, MonacoTheme.Space.m)
            content()
        }
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .padding(MonacoTheme.Space.m)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                MonacoTheme.surface,
                in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                    .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
            }
            .padding(.horizontal, MonacoTheme.Space.m)
    }
}

/// Pressed state for a matchup card: it dims while held, no scale.
struct MatchupCardButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.7 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}
