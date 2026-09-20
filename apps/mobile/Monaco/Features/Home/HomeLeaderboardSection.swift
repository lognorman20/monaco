import MonacoCore
import SwiftUI

/// "Top investors": the cross-cabal people board for the selected window.
///
/// The rows come from the shared dashboard; `model` owns which window they describe, so the
/// chip, the rows and the copy under an empty board can never disagree (#275, #276).
struct HomeLeaderboardSection: View {
    @ObservedObject var auth: DynamicAuthService
    let model: HomeLeaderboardModel
    let people: [HomePeopleBoardRowDTO]
    /// Whether the member is in any cabal, which decides why an empty board is empty.
    let hasCabals: Bool
    let onSelect: (HomeLeaderboardRange) -> Void
    let onRetry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Top investors")

            rangeChips

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

    /// A range pill on `surfaceSunken`, 44 pt tall so it can be hit, unlike the caption-sized
    /// chip it replaces.
    private struct RangeChip: View {
        let title: String
        let isSelected: Bool

        var body: some View {
            Text(title)
                .font(MonacoTheme.Typo.callout.weight(.semibold))
                .foregroundStyle(isSelected ? MonacoTheme.primaryButtonLabel : MonacoTheme.muted)
                .lineLimit(1)
                .padding(.horizontal, 16)
                .frame(minWidth: 56, minHeight: 44)
                .background(Capsule().fill(isSelected ? MonacoTheme.primaryButtonFill : MonacoTheme.surfaceSunken))
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

    private var loadingBoard: some View {
        VStack(spacing: MonacoTheme.Space.s) {
            ForEach(0..<3, id: \.self) { _ in
                SkeletonBlock(height: 60, radius: MonacoTheme.Radius.card)
            }
        }
        .accessibilityIdentifier("home-leaderboard-loading")
    }

    /// Rows already on screen stay put while the next window loads — dimmed, so nobody reads
    /// last window's numbers as this one's.
    private var board: some View {
        MonacoGroupedList {
            ForEach(people) { row in
                NavigationLink {
                    UserProfileGroupsView(
                        auth: auth,
                        userId: row.userId,
                        displayName: row.displayName,
                        profilePhotoUrl: row.profilePhotoUrl
                    )
                } label: {
                    MonacoRow(
                        title: row.displayName,
                        chevron: true,
                        isLast: row.userId == people.last?.userId,
                        leading: { MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 44) },
                        trailing: {
                            PercentText(percentReturn: row.percentReturn, style: .row)
                            PnLText(dollarPnl: row.dollarPnl, style: .caption)
                        }
                    )
                }
                .buttonStyle(.monacoRow)
                .accessibilityIdentifier("home-leaderboard-row-\(row.userId)")
            }
        }
        .opacity(model.isLoading ? 0.4 : 1)
        .overlay {
            if model.isLoading {
                ProgressView()
                    .tint(MonacoTheme.ink)
                    .accessibilityIdentifier("home-leaderboard-loading")
            }
        }
        .animation(.easeInOut(duration: 0.15), value: model.isLoading)
        .disabled(model.isLoading)
    }
}
