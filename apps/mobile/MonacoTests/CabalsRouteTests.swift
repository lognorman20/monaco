import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// A discovery row resolves to one route, once, at tap time (#293).
@MainActor
struct CabalsRouteTests {
    @Test func aRowTheViewerIsInOpensTheCabal() {
        let route = CabalsRoute(row: "g1", name: "Weekend investors", isJoined: true, joinMode: .request)

        #expect(route == .cabal(id: "g1", name: "Weekend investors", from: .plainPush))
    }

    /// A discovery row has no `.matchedTransitionSource` behind it, so it must not ask for the
    /// zoom — otherwise it grows out of whichever strip card happens to be rendered for that id.
    @Test func aDiscoveryRowNeverClaimsAZoomSource() {
        let route = CabalsRoute(row: "g1", name: "Weekend investors", isJoined: true, joinMode: .open)

        guard case let .cabal(_, _, origin) = route else {
            Issue.record("expected a cabal route")
            return
        }
        #expect(origin == .plainPush)
    }

    /// The origin is part of the route's identity: the same cabal reached from the strip and from
    /// the board are two different pushes, and `navigationDestination(item:)` must see that.
    @Test func theOriginDistinguishesTwoPushesOfTheSameCabal() {
        #expect(
            CabalsRoute.cabal(id: "g1", name: "Weekend investors", from: .stripCard)
                != .cabal(id: "g1", name: "Weekend investors", from: .plainPush)
        )
    }

    @Test func anApprovalCabalRoutesToTheRequestForm() {
        let route = CabalsRoute(row: "g2", name: "Rent money", isJoined: false, joinMode: .request)

        #expect(route == .join(id: "g2", name: "Rent money", mode: .request))
    }

    @Test func anOpenCabalRoutesToTheJoinForm() {
        let route = CabalsRoute(row: "g3", name: "Apple heads", isJoined: false, joinMode: .open)

        #expect(route == .join(id: "g3", name: "Apple heads", mode: .open))
    }
}
