import MonacoCore
import SwiftUI

/// "Top investors": the cross-cabal people board for the selected window.
///
/// The rows come from the shared dashboard; `model` owns which window they describe, so the
/// control, the rows and the copy under an empty board can never disagree (#275, #276).
///
/// The hand-rolled range chips are gone: this is `MonacoSegmented` over the same
/// `HomeLeaderboardRange`, so the app has **one** selection vocabulary rather than a pill row here
/// and a segmented control three screens away.
struct HomeLeaderboardSection: View {
    @ObservedObject var auth: DynamicAuthService
    let model: HomeLeaderboardModel
    let people: [HomePeopleBoardRowDTO]
    /// Whether the member is in any cabal, which decides why an empty board is empty.
    let hasCabals: Bool
    let onSelect: (HomeLeaderboardRange) -> Void
    let onRetry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.headerToContent) {
            MonacoSectionHeader("Top investors")

            MonacoSegmented(
                HomeLeaderboardRange.allCases,
                selection: Binding(get: { model.selectedRange }, set: onSelect)
            ) { $0.label }
            .accessibilityLabel("Leaderboard window")
            .accessibilityIdentifier("home-leaderboard-range")

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
                SkeletonBlock(height: 60, radius: MonacoTheme.Radius.container)
            }
        }
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
                    MonacoRow(
                        title: row.displayName,
                        chevron: true,
                        isLast: row.userId == people.last?.userId,
                        leading: {
                            MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 44)
                                .overlay(alignment: .bottomLeading) {
                                    RankBadge(rank: index + 1)
                                        .offset(x: -4, y: 4)
                                }
                        },
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
                // Its own identifier: a refresh over rows already on screen is not the same
                // thing as an empty board loading, and a UI test matching one identifier for
                // both could not tell them apart.
                ProgressView()
                    .tint(MonacoTheme.controlTint)
                    .accessibilityIdentifier("home-leaderboard-refreshing")
            }
        }
        .animation(.easeInOut(duration: 0.15), value: model.isLoading)
        .disabled(model.isLoading)
    }
}

/// The podium badge on the leaderboard avatar.
///
/// Ranks 1–3 get **one** treatment, keyed off the rank itself: an ink disc, with `trophy.fill`
/// on rank 1 (§5.2.3). Ranks 4 and beyond keep a grey numeral — a board where everything is
/// decorated has no podium.
///
/// It is deliberately *not* tinted. §1.8 scopes `CabalTint` to cabal identity, and hashing a
/// person's id through that ramp made the colour mean nothing: rank 1 sage, rank 2 peach, at
/// random, with two adjacent rows free to land on the same tint. Position is the content here,
/// so the badge says position and nothing else. Ink is also the neutral emphasis the palette
/// already has — brand would claim a tap and green would claim a profit.
///
/// The disc and the numeral both scale with Dynamic Type, capped so the badge stays a badge
/// rather than outgrowing the 44pt avatar it is pinned to.
///
/// VoiceOver reads it: the row's own `.accessibilityElement(children: .combine)` picks this up
/// ahead of the name and the returns, so the board speaks "Number 1, Ana, up 12.4 percent, …".
/// Hiding it was what cost the figures their place in the label.
private struct RankBadge: View {
    let rank: Int

    @ScaledMetric(relativeTo: .footnote) private var scaledDisc: CGFloat = 22
    @ScaledMetric(relativeTo: .footnote) private var scaledNumeral: CGFloat = 13

    private var isPodium: Bool { rank <= 3 }
    private var disc: CGFloat { min(scaledDisc, 34) }
    private var numeral: CGFloat { min(scaledNumeral, 20) }

    var body: some View {
        Group {
            if rank == 1 {
                Image(systemName: "trophy.fill")
                    .font(.system(size: numeral * 0.85, weight: .bold))
            } else {
                Text("\(rank)")
                    .font(.system(size: numeral, weight: .bold).monospacedDigit())
            }
        }
        .foregroundStyle(isPodium ? MonacoTheme.bgRaised : MonacoTheme.fgMuted)
        .frame(width: disc, height: disc)
        .background {
            Circle()
                .fill(isPodium ? MonacoTheme.fgPrimary : MonacoTheme.fillQuiet)
        }
        .overlay(Circle().strokeBorder(MonacoTheme.bgRaised, lineWidth: 2))
        .accessibilityLabel("Number \(rank)")
    }
}
