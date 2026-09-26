import MonacoCore
import SwiftUI

/// The season table: every cabal with a finished week, by wins and then by the sum of its weekly
/// returns. The member's cabals are washed so they can be found, and the leader wears the crown.
struct MatchupTableView: View {
    let source: any MatchupDataSource

    @State private var model = MatchupTableModel()

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .monacoCanvas()
            .navigationTitle(MatchupCopy.tableTitle)
            .navigationBarTitleDisplayMode(.inline)
            .task { try? await model.load(from: source) }
            .refreshable { try? await model.load(from: source) }
            .pollWhileVisible(every: .seconds(60), isActive: model.state.value != nil) {
                try await model.load(from: source, quiet: true)
            }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading, .hidden:
            ScrollView {
                VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                    SkeletonBlock(width: 180, height: 12)
                        .padding(.horizontal, MonacoTheme.Space.m)
                    BoardRowSkeleton(rows: 6)
                }
                .padding(.top, MonacoTheme.Space.m)
            }
            .accessibilityIdentifier("matchup-table-loading")
        case .failed:
            ScrollView {
                EmptyState(
                    title: MatchupCopy.tableLoadFailedTitle,
                    actionTitle: MatchupCopy.tryAgain,
                    action: { Task { try? await model.load(from: source) } }
                )
                .padding(.top, MonacoTheme.Space.xl)
            }
            .accessibilityIdentifier("matchup-table-error")
        case .loaded(let table):
            if table.cabals.isEmpty {
                ScrollView {
                    EmptyState(title: MatchupCopy.tableEmptyTitle, message: MatchupCopy.tableEmptyDetail)
                        .padding(.top, MonacoTheme.Space.xl)
                }
                .accessibilityIdentifier("matchup-table-empty")
            } else {
                loaded(table)
            }
        }
    }

    private func loaded(_ table: MatchupTableDTO) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
                HStack(alignment: .firstTextBaseline) {
                    if let through = table.throughWeek {
                        Text("Through the week of \(MatchupCopy.weekLabel(through))")
                            .font(MonacoTheme.Typo.stamp)
                            .foregroundStyle(MonacoTheme.tertiaryText)
                    }
                    Spacer()
                    BoardRowRecordHeader()
                }
                .padding(.horizontal, MonacoTheme.Space.m)

                MonacoGroupedList {
                    ForEach(table.cabals) { row in
                        BoardRow(
                            rank: row.rank,
                            name: row.name,
                            detail: row.streak.map { "Streak \($0)" },
                            percentReturn: nil,
                            isViewer: row.isMine,
                            isLast: row.id == table.cabals.last?.id,
                            record: BoardRowRecord(wins: row.wins, losses: row.losses, ties: row.ties)
                        ) {
                            CabalMark(groupId: row.groupID, name: row.name, size: 40, pictureUrl: row.pictureUrl)
                        }
                        .accessibilityIdentifier("matchup-table-row-\(row.groupID)")
                    }
                }
            }
            .padding(.top, MonacoTheme.Space.m)
            .padding(.bottom, MonacoTheme.Space.xl)
        }
        .accessibilityIdentifier("matchup-table")
    }
}
