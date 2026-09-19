import MonacoCore
import SwiftUI

struct HomeLeaderboardSection: View {
    @ObservedObject var auth: PrivyAuthService
    @Binding var range: HomeLeaderboardRange
    let people: [HomePeopleBoardRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            MonacoSectionHeader("Top investors")

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(HomeLeaderboardRange.allCases, id: \.self) { option in
                        Button {
                            range = option
                        } label: {
                            MonacoChip(title: option.label, isSelected: range == option)
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("home-leaderboard-range-\(option.rawValue)")
                    }
                }
            }

            if people.isEmpty {
                EmptyState(
                    title: "No investors yet",
                    message: "Join a cabal to see members on the leaderboard."
                )
            } else {
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
            }
        }
    }
}
