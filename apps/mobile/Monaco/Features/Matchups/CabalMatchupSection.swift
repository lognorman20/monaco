import MonacoCore
import SwiftUI

/// The cabal screen's "Matchup" section, after the pot: this week's two scores, the season
/// record, and the season table. A ruled section on the paper, not a card: the scoreboard row
/// opens the full matchup.
struct CabalMatchupSection: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let groupName: String

    @Environment(\.matchupDataSource) private var injectedSource
    @State private var model: GroupMatchupModel

    init(auth: PrivyAuthService, groupId: String, groupName: String) {
        self.auth = auth
        self.groupId = groupId
        self.groupName = groupName
        _model = State(initialValue: GroupMatchupModel(groupId: groupId))
    }

    private var source: any MatchupDataSource {
        injectedSource ?? LiveMatchupDataSource(auth: auth)
    }

    var body: some View {
        content
            .task(id: groupId) { try? await model.load(from: source) }
            .pollWhileVisible(every: LiveRefreshCadence.resting, isActive: model.state.value != nil) {
                try await model.load(from: source, quiet: true)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .hidden:
            EmptyView()
        case .loading:
            section {
                MonacoGroupedList {
                    MatchupScoreboardSkeleton()
                        .padding(MonacoTheme.Space.m)
                }
            }
            .accessibilityIdentifier("cabal-matchup-loading")
        case .failed:
            section {
                EmptyState(
                    title: MatchupCopy.loadFailedTitle,
                    actionTitle: MatchupCopy.tryAgain,
                    action: { Task { try? await model.load(from: source) } }
                )
            }
            .accessibilityIdentifier("cabal-matchup-error")
        case .loaded(let dto):
            section {
                MonacoGroupedList {
                    NavigationLink {
                        MatchupView(auth: auth, groupId: groupId, groupName: groupName, source: source, initial: dto)
                    } label: {
                        currentRow(dto)
                    }
                    .buttonStyle(.monacoRow)
                    .accessibilityIdentifier("cabal-matchup-open")

                    if let incoming = dto.challenges.first(where: \.canAccept) {
                        NavigationLink {
                            MatchupView(auth: auth, groupId: groupId, groupName: groupName, source: source, initial: dto)
                        } label: {
                            MonacoRow(title: incoming.opponent.name, subtitle: "Wants to play you next week", subtitleColor: MonacoTheme.warning, chevron: true) {
                                CabalMark(groupId: incoming.opponent.groupID, name: incoming.opponent.name, size: 44, pictureUrl: incoming.opponent.pictureUrl)
                            }
                        }
                        .buttonStyle(.monacoRow)
                        .accessibilityIdentifier("cabal-matchup-incoming")
                    }

                    recordRow(dto)
                }
            }
            .accessibilityIdentifier("cabal-matchup")
        }
    }

    private func section<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            HStack(alignment: .firstTextBaseline, spacing: MonacoTheme.Space.s) {
                MonacoSectionHeader(MatchupCopy.cabalSectionTitle)
                NavigationLink {
                    MatchupTableView(source: source)
                } label: {
                    Text(MatchupCopy.tableTitle)
                        .font(MonacoTheme.Typo.calloutStrong)
                        .foregroundStyle(MonacoTheme.brand)
                        .lineLimit(1)
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("cabal-matchup-table")
            }
            .padding(.horizontal, MonacoTheme.Space.m)
            content()
        }
    }

    @ViewBuilder
    private func currentRow(_ dto: GroupMatchupDTO) -> some View {
        HStack(spacing: MonacoTheme.Space.s) {
            if let current = dto.current {
                MatchupScoreboard(matchup: current, showsChevron: true)
            } else {
                VStack(alignment: .leading, spacing: 2) {
                    Text(MatchupCopy.notInDrawTitle)
                        .font(MonacoTheme.Typo.rowTitle)
                        .foregroundStyle(MonacoTheme.ink)
                    Text(MatchupCopy.notInDrawDetail)
                        .font(MonacoTheme.Typo.caption)
                        .foregroundStyle(MonacoTheme.secondaryText)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(MonacoTheme.tertiaryText)
                    .accessibilityHidden(true)
            }
        }
        .padding(MonacoTheme.Space.m)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) {
            MonacoRule().padding(.leading, MonacoTheme.Space.m)
        }
    }

    private func recordRow(_ dto: GroupMatchupDTO) -> some View {
        HStack(spacing: MonacoTheme.Space.sm) {
            Text("Season record")
                .font(MonacoTheme.Typo.callout)
                .foregroundStyle(MonacoTheme.secondaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(MatchupCopy.record(dto.record))
                .font(MonacoTheme.Typo.dataStrong)
                .foregroundStyle(dto.record.hasPlayed ? MonacoTheme.ink : MonacoTheme.tertiaryText)
                .monospacedDigit()
        }
        .padding(.horizontal, MonacoTheme.Space.m)
        .frame(minHeight: 52)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Season record, \(MatchupCopy.recordSpoken(dto.record))")
        .accessibilityIdentifier("cabal-matchup-record")
    }
}
