import MonacoCore
import SwiftUI

struct HomeLeaderboardSection: View {
    @ObservedObject var auth: PrivyAuthService
    @Binding var range: HomeLeaderboardRange
    let people: [HomePeopleBoardRowDTO]

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Top investors")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)

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
                MonacoEmptyStateCard(
                    message: "Join a cabal to see members on the leaderboard.",
                    systemImage: "chart.bar"
                )
            } else {
                ForEach(people) { row in
                    NavigationLink {
                        UserProfileGroupsView(
                            auth: auth,
                            userId: row.userId,
                            displayName: row.displayName,
                            profilePhotoUrl: row.profilePhotoUrl
                        )
                    } label: {
                        MonacoRowCard(
                            title: row.displayName,
                            subtitle: PercentReturnFormatter.format(row.percentReturn),
                            trailing: row.dollarPnl,
                            subtitleColor: MonacoTheme.signed(row.percentReturn),
                            trailingColor: MonacoTheme.signed(row.dollarPnl)
                        ) {
                            MonacoAvatar(photoURL: row.profilePhotoUrl, displayName: row.displayName, size: 44)
                        }
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("home-leaderboard-row-\(row.userId)")
                }
            }
        }
    }
}
