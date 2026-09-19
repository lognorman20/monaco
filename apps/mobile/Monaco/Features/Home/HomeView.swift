import MonacoCore
import SwiftUI

/// App home with cabal board and people board.
struct HomeView: View {
    @ObservedObject var auth: PrivyAuthService
    @Environment(AppSessionStore.self) private var session

    @State private var selectedTab = 0

    private var home: HomeViewDTO {
        session.home ?? HomeViewDTO(groups: [], people: [])
    }

    private var joinedCabals: [HomeGroupBoardRowDTO] {
        session.joinedCabals
    }

    var body: some View {
        VStack(spacing: 0) {
            PlatformBalanceCard(balance: session.platformBalance, isLoading: session.isBalanceLoading)
                .padding([.horizontal, .top])

            NavigationLink {
                DepositView(auth: auth, joinedCabals: joinedCabals)
            } label: {
                Label("Deposit", systemImage: "plus.circle")
                    .font(.subheadline.weight(.semibold))
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.monacoSecondary)
            .padding(.horizontal)
            .padding(.bottom, 8)
            .accessibilityIdentifier("home-deposit-link")

            Picker("Board", selection: $selectedTab) {
                Text("Cabals").tag(0)
                Text("People").tag(1)
            }
            .pickerStyle(.segmented)
            .monacoSegmentedBoardPicker()

            if selectedTab == 0 {
                CabalListSection(
                    auth: auth,
                    rows: home.groups,
                    onRefresh: { await session.refresh(auth: auth) },
                    emptyMessage: "No cabals yet. Create or join one to start investing together."
                )
            } else {
                peopleBoardSection
            }
        }
        .monacoCanvas()
        .navigationTitle("Home")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    NavigationLink {
                        CreateGroupView(auth: auth)
                    } label: {
                        Label("Create cabal", systemImage: "plus")
                    }
                    NavigationLink {
                        JoinGroupView(auth: auth)
                    } label: {
                        Label("Join cabal", systemImage: "person.badge.plus")
                    }
                } label: {
                    Image(systemName: "plus.circle")
                        .monacoToolbarIcon()
                }
                .accessibilityIdentifier("home-club-menu")
            }
        }
        .refreshable {
            await session.refresh(auth: auth)
        }
    }

    private var peopleBoardSection: some View {
        List {
            if home.people.isEmpty {
                MonacoEmptyStateCard(
                    message: peopleEmptyMessage,
                    systemImage: "chart.bar"
                )
            } else {
                ForEach(home.people) { row in
                    NavigationLink {
                        UserProfileGroupsView(
                            auth: auth,
                            userId: row.userId,
                            displayName: row.displayName
                        )
                    } label: {
                        MonacoRowCard(
                            systemImage: "person.fill",
                            title: row.displayName,
                            subtitle: PercentReturnFormatter.format(row.percentReturn),
                            trailing: row.dollarPnl
                        )
                    }
                    .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .accessibilityIdentifier("home-people-row-\(row.userId)")
                }
            }
        }
        .monacoInsetList()
    }

    private var peopleEmptyMessage: String {
        if home.groups.isEmpty {
            return "Join a cabal to see members on the leaderboard."
        }
        return "No members in your cabals yet."
    }
}

#Preview {
    let session = AppSessionStore()
    session.home = HomeViewDTO(
        groups: [
            HomeGroupBoardRowDTO(
                groupId: "g1",
                name: "Weekend investors",
                potValueUsd: "548.20",
                percentReturn: "0.124",
                dollarPnl: "+48.20",
                isJoined: true
            ),
        ],
        people: [
            HomePeopleBoardRowDTO(
                userId: "u1",
                displayName: "Alfred",
                percentReturn: "0.124",
                dollarPnl: "+48.20"
            ),
        ]
    )
    return NavigationStack {
        HomeView(auth: PrivyAuthService())
            .environment(session)
            .monacoRootAppearance()
    }
}
