import MonacoCore
import SwiftUI

/// Every funded cabal on Monaco, ranked by percent return.
struct CabalsLeaderboardSection: View {
    @ObservedObject var auth: PrivyAuthService
    let model: CabalsTabModel
    var onChanged: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: MonacoTheme.Space.s) {
            Text("Top cabals")
                .font(MonacoTheme.TypeRole.title)
                .foregroundStyle(MonacoTheme.ink)
            Text("Ranked by return across everyone on Monaco")
                .monacoSecondaryCaption()

            if model.isLeaderboardLoading {
                ProgressView()
                    .tint(MonacoTheme.accent)
                    .frame(maxWidth: .infinity, minHeight: 80)
                    .accessibilityIdentifier("cabals-leaderboard-loading")
            } else if model.leaderboardFailed, model.leaderboard.isEmpty {
                VStack(spacing: MonacoTheme.Space.s) {
                    Text("Couldn't load the board.")
                        .font(MonacoTheme.TypeRole.body)
                        .foregroundStyle(MonacoTheme.muted)
                    Button("Try again") {
                        Task { await model.loadLeaderboard() }
                    }
                    .buttonStyle(.monacoSecondary)
                    .accessibilityIdentifier("cabals-leaderboard-retry")
                }
                .frame(maxWidth: .infinity)
                .monacoSurfaceCard()
                .accessibilityIdentifier("cabals-leaderboard-error")
            } else if model.leaderboard.isEmpty {
                MonacoEmptyStateCard(
                    message: "No cabal has put money in yet. The first one to fund takes the top spot.",
                    systemImage: "trophy"
                )
                .accessibilityIdentifier("cabals-leaderboard-empty")
            } else {
                LazyVStack(spacing: MonacoTheme.Space.s) {
                    ForEach(model.leaderboard) { row in
                        NavigationLink {
                            CabalDiscoveryDestinationView(
                                auth: auth,
                                groupId: row.groupID,
                                name: row.name,
                                destination: GroupDiscoveryDestination(isJoined: row.isJoined, joinMode: row.joinMode),
                                onChanged: onChanged
                            )
                        } label: {
                            CabalBoardRow(
                                leading: "\(row.rank)",
                                name: row.name,
                                detail: cabalRowDetail(memberCount: row.memberCount, isJoined: row.isJoined, joinMode: row.joinMode),
                                potValueUsd: row.potValueUsd,
                                dollarPnl: row.dollarPnl,
                                percentReturn: row.percentReturn
                            )
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier("cabals-leaderboard-row-\(row.groupID)")
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("cabals-leaderboard")
            }
        }
    }
}

func cabalRowDetail(memberCount: Int, isJoined: Bool, joinMode: GroupJoinMode) -> String {
    let members = memberCount == 1 ? "1 member" : "\(memberCount) members"
    if isJoined { return "\(members) · Joined" }
    switch joinMode {
    case .open: return "\(members) · Open"
    case .request: return "\(members) · Approval"
    }
}

/// Shared row for the board and search results.
struct CabalBoardRow: View {
    let leading: String?
    let name: String
    let detail: String
    let potValueUsd: String
    let dollarPnl: String
    let percentReturn: String?

    var body: some View {
        HStack(spacing: 12) {
            if let leading {
                Text(leading)
                    .font(MonacoTheme.TypeRole.title.monospacedDigit())
                    .foregroundStyle(MonacoTheme.accent)
                    .frame(minWidth: 22, alignment: .leading)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(name)
                    .font(MonacoTheme.TypeRole.body.weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                Text(detail)
                    .font(MonacoTheme.TypeRole.caption)
                    .foregroundStyle(MonacoTheme.muted)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
            }
            .layoutPriority(1)
            Spacer(minLength: MonacoTheme.Space.s)
            VStack(alignment: .trailing, spacing: 2) {
                Text(UsdAmountFormatter.format(decimalString: potValueUsd))
                    .font(.subheadline.monospacedDigit().weight(.semibold))
                    .foregroundStyle(MonacoTheme.ink)
                CabalPnLLabel(dollarPnl: dollarPnl, percentReturn: percentReturn)
            }
            .fixedSize()
        }
        .padding(MonacoTheme.Space.m)
        .frame(minHeight: 44)
        .background(
            MonacoTheme.surface,
            in: RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
        )
        .overlay {
            RoundedRectangle(cornerRadius: MonacoTheme.Radius.card, style: .continuous)
                .strokeBorder(MonacoTheme.hairline, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Where a discovery row leads: the cabal itself for members, otherwise the
/// join flow for that cabal's join policy.
struct CabalDiscoveryDestinationView: View {
    @ObservedObject var auth: PrivyAuthService
    let groupId: String
    let name: String
    let destination: GroupDiscoveryDestination
    var onChanged: () async -> Void

    var body: some View {
        switch destination {
        case .detail:
            GroupDetailView(auth: auth, groupId: groupId, groupName: name, onLeft: onChanged)
        case .join:
            JoinGroupView(auth: auth, groupId: groupId, groupName: name, joinMode: .open)
        case .requestToJoin:
            JoinGroupView(auth: auth, groupId: groupId, groupName: name, joinMode: .request)
        }
    }
}
