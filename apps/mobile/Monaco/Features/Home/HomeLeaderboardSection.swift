import MonacoCore
import SwiftUI

/// "Top investors": the cross-cabal people board for the selected window.
///
/// The rows come from the shared dashboard; `model` owns which window they describe, so the
/// chip, the rows and the copy under an empty board can never disagree (#275, #276).
struct HomeLeaderboardSection: View {
    @ObservedObject var auth: PrivyAuthService
    let model: HomeLeaderboardModel
    let people: [HomePeopleBoardRowDTO]
    /// Whether the member is in any cabal, which decides why an empty board is empty.
    let hasCabals: Bool
    let onSelect: (HomeLeaderboardRange) -> Void
    let onRetry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Top investors")
                .padding(.horizontal, MonacoTheme.Space.m)

            rangeChips
                .padding(.horizontal, MonacoTheme.Space.m)
                .padding(.bottom, MonacoTheme.Space.xs)

            if model.failed {

                EmptyState(
                    title: "Couldn't load the board",
                    message: "Tap try again, or pull down to refresh Home.",
                    actionTitle: "Try again",
                    action: onRetry
                )
                .accessibilityIdentifier("home-leaderboard-error")
            } else if people.isEmpty {
                emptyBoard
            } else {
                board
            }
        }
    }

    private var rangeChips: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(HomeLeaderboardRange.allCases, id: \.self) { option in
                    let isSelected = model.selectedRange == option
                    Button {
                        onSelect(option)
                    } label: {
                        RangeChip(title: option.label, isSelected: isSelected)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(option.accessibilityName)
                    .accessibilityAddTraits(isSelected ? [.isSelected] : [])
                    .accessibilityIdentifier("home-leaderboard-range-\(option.rawValue)")
                }
            }
        }
    }

    /// A range chip in the market's voice — it names a window of time, which is data. Drawn
    /// 34pt tall and padded out to a 44pt target, so the row of five reads as a control strip
    /// rather than as five buttons.
    private struct RangeChip: View {
        let title: String
        let isSelected: Bool

        var body: some View {
            Text(title)
                .font(MonacoTheme.Typo.dataCaption)
                .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.muted)
                .lineLimit(1)
                .padding(.horizontal, 14)
                .frame(minWidth: 48, minHeight: 34)
                .background(Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
                .padding(.vertical, 5)
                .contentShape(Capsule())
        }
    }


    /// An empty board is almost never "nobody has joined": ranged windows drop everyone whose
    /// cabal has no snapshot from before the window started, which is every brand-new cabal.
    @ViewBuilder
    private var emptyBoard: some View {
        if model.isLoading {
            loadingBoard
        } else if !hasCabals {
            EmptyState(
                title: "No investors yet",
                message: "Join a cabal to see members on the leaderboard."
            )
            .accessibilityIdentifier("home-leaderboard-empty")
        } else {
            EmptyState(
                title: "Nothing to rank yet",
                message: "No returns for \(model.selectedRange.windowPhrase) yet. Try All."
            )
            .accessibilityIdentifier("home-leaderboard-empty-range")
        }
    }

    /// Three rows in the shape of `BoardRow` — rank, face, name, figure — between the rules
    /// the board will draw, so nothing jumps when the people arrive.
    private var loadingBoard: some View {
        BoardRowSkeleton(rows: 3)
            .accessibilityIdentifier("home-leaderboard-loading")
    }

    /// Rows already on screen stay put while the next window loads — dimmed, so nobody reads
    /// last window's numbers as this one's.
    private var board: some View {
        MonacoGroupedList {
            ForEach(Array(people.enumerated()), id: \.element.userId) { index, row in
                NavigationLink {
                    UserProfileGroupsView(
                        auth: auth,
                        userId: row.userId,
                        displayName: row.displayName,
                        profilePhotoUrl: row.profilePhotoUrl
                    )
                } label: {
                    BoardRow(
                        rank: index + 1,
                        name: row.displayName,
                        percentReturn: row.percentReturn,
                        dollarPnl: row.dollarPnl,
                        isLast: row.userId == people.last?.userId
                    ) {
                        MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 40, seed: row.userId)
                    }
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("home-leaderboard-row-\(row.userId)")
            }
        }

        .opacity(model.isLoading ? 0.4 : 1)
        .overlay {
            if model.isLoading {
                // Its own identifier: a refresh over rows already on screen is not the same
                // thing as an empty board loading, and a UI test matching one identifier for
                // both could not tell them apart.
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .accessibilityIdentifier("home-leaderboard-refreshing")
            }
        }
        .animation(.easeInOut(duration: 0.15), value: model.isLoading)
        .disabled(model.isLoading)
    }
}
