import MonacoCore
import SwiftUI

/// Every screen the Cabals tab can push.
///
/// The tab owns one route value, and rows hand over a route instead of a view.
/// Two things follow from that:
///
/// - A destination is decided once, when the row is tapped. A board reload that
///   flips a row to "you're in" can no longer swap the screen a member is
///   already reading.
/// - The tab can *replace* the top screen. "New cabal" becomes the cabal that
///   was just created, and a join screen becomes the cabal that was just
///   joined, so Back always lands on the tab — never on a live form that would
///   create a second cabal.
enum CabalsRoute: Hashable, Identifiable {
    /// A cabal the viewer is already in. The name is what the row that pushed
    /// it knew; the invite-code route has none until the cabal loads.
    case cabal(id: String, name: String?)
    /// A cabal picked from the board or search that the viewer is not in yet.
    case join(id: String, name: String, mode: GroupJoinMode)
    /// Join by pasting an invite code a friend shared.
    case joinByCode
    /// The "New cabal" form.
    case create

    var id: Self { self }

    /// The route a discovery row leads to, from what that row knows.
    init(row groupId: String, name: String, isJoined: Bool, joinMode: GroupJoinMode) {
        switch GroupDiscoveryDestination(isJoined: isJoined, joinMode: joinMode) {
        case .detail: self = .cabal(id: groupId, name: name)
        case .join: self = .join(id: groupId, name: name, mode: .open)
        case .requestToJoin: self = .join(id: groupId, name: name, mode: .request)
        }
    }
}

/// Builds the screen behind a route. The Cabals tab declares this once, so no
/// row view ever constructs a destination.
struct CabalsRouteDestination: View {
    @ObservedObject var auth: DynamicAuthService
    let route: CabalsRoute
    /// The create and join writes, so the tab's sample harness can drive both
    /// flows end to end without a backend.
    let actions: CabalsActionSource
    /// The viewer joined, left, or created a cabal: reload the tab.
    var onChanged: () async -> Void
    /// A cabal was created; the owner replaces this screen with it.
    var onCreated: (CreateGroupResponse) -> Void
    /// The viewer is now a member; the owner replaces this screen with the cabal.
    var onJoined: (_ groupId: String, _ groupName: String?) -> Void

    var body: some View {
        switch route {
        case let .cabal(id, name):
            GroupDetailView(auth: auth, groupId: id, groupName: name, onLeft: onChanged)
        case let .join(id, name, mode):
            JoinGroupView(
                auth: auth, groupId: id, groupName: name, joinMode: mode,
                actions: actions, onJoined: onJoined
            )
        case .joinByCode:
            JoinGroupView(auth: auth, actions: actions, onJoined: onJoined)
        case .create:
            CreateGroupView(auth: auth, actions: actions, onCreated: onCreated)
        }
    }
}
