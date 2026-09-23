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
                                    RankBadge(rank: index + 1, seed: row.userId)
                                        .offset(x: -4, y: 4)
                                }
                        },
                        trailing: {
                            PercentText(percentReturn: row.percentReturn, style: .row)
                            PnLText(dollarPnl: row.dollarPnl, style: .caption)
                        }
                    )
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel("Number \(index + 1), \(row.displayName)")
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
/// Ranks 1–3 only, and on `CabalTint.cta` rather than `fill`: this is a 13pt bold numeral, which
/// is **not** "large text" under WCAG, so it needs the 4.5:1 ramp. Ranks 4 and beyond keep a grey
/// numeral — a board where everything is decorated has no podium.
///
/// The tint is hashed from the person's own id, so a member's badge is the same colour every time
/// the board is drawn and two people next to each other are unlikely to share one. It is never the
/// only signal: the numeral is the rank, and the name is right beside it.
private struct RankBadge: View {
    let rank: Int
    let seed: String

    private var isPodium: Bool { rank <= 3 }

    var body: some View {
        Text("\(rank)")
            .font(.system(size: 13, weight: .bold).monospacedDigit())
            .foregroundStyle(isPodium ? Color.white : MonacoTheme.fgMuted)
            .frame(width: 22, height: 22)
            .background {
                Circle()
                    .fill(isPodium ? MonacoTheme.CabalTint.forGroupId(seed).cta : MonacoTheme.fillQuiet)
            }
            .overlay(Circle().strokeBorder(MonacoTheme.bgRaised, lineWidth: 2))
            .accessibilityHidden(true)
    }
}
