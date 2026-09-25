import Foundation
import MonacoCore
import Testing
@testable import Monaco

/// A discovery row resolves to one route, once, at tap time (#293).
@MainActor
struct CabalsRouteTests {
    @Test func aRowTheViewerIsInOpensTheCabal() {
        let route = CabalsRoute(row: "g1", name: "Weekend investors", isJoined: true, joinMode: .request)

        #expect(route == .cabal(id: "g1", name: "Weekend investors"))
    }

    @Test func anApprovalCabalRoutesToTheRequestForm() {
        let route = CabalsRoute(row: "g2", name: "Rent money", isJoined: false, joinMode: .request)

        #expect(route == .join(id: "g2", name: "Rent money", mode: .request, memberCount: nil, pictureUrl: nil))
    }

    @Test func anOpenCabalRoutesToTheJoinForm() {
        let route = CabalsRoute(row: "g3", name: "Apple heads", isJoined: false, joinMode: .open)

        #expect(route == .join(id: "g3", name: "Apple heads", mode: .open, memberCount: nil, pictureUrl: nil))
    }

    /// The join screen leads with the cabal's face and size, so the row hands both over.
    @Test func aJoinRouteCarriesWhatTheRowKnew() {
        let route = CabalsRoute(
            row: "g4", name: "Semis or bust", isJoined: false, joinMode: .open,
            memberCount: 5, pictureUrl: "https://example.com/semis.png"
        )
        #expect(route == .join(id: "g4", name: "Semis or bust", mode: .open, memberCount: 5, pictureUrl: "https://example.com/semis.png"))
    }
}
