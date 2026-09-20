import MonacoCore
import Testing
@testable import Monaco

struct ProposalFeedPollingTests {
    private func open(_ ids: [String]) -> [ProposalDTO] {
        ids.map { ProposalDTO(id: $0, symbol: "AAPLx", status: "open", kind: "buy", canVote: true) }
    }

    @Test func theFirstTickFillsTheClosedTab() {
        #expect(ProposalFeedPolling.shouldReloadClosed(
            previousOpen: open(["a"]), freshOpen: open(["a"]), visibleTab: .open, hasClosed: false
        ))
    }

    @Test func anOpenVoteDoesNotDragTheWholeHistoryDownEveryTick() {
        // The whole point: a five-second Open poll must not re-read the cabal's closed history.
        #expect(!ProposalFeedPolling.shouldReloadClosed(
            previousOpen: open(["a", "b"]), freshOpen: open(["a", "b"]), visibleTab: .open, hasClosed: true
        ))
    }

    @Test func aNewProposalAloneIsNotAReasonToRereadClosed() {
        #expect(!ProposalFeedPolling.shouldReloadClosed(
            previousOpen: open(["a"]), freshOpen: open(["a", "b"]), visibleTab: .open, hasClosed: true
        ))
    }

    @Test func aProposalLeavingTheOpenListMeansTheClosedTabIsStale() {
        #expect(ProposalFeedPolling.shouldReloadClosed(
            previousOpen: open(["a", "b"]), freshOpen: open(["b"]), visibleTab: .open, hasClosed: true
        ))
    }

    @Test func theClosedTabIsKeptFreshWhileItIsTheOneBeingRead() {
        #expect(ProposalFeedPolling.shouldReloadClosed(
            previousOpen: open(["a"]), freshOpen: open(["a"]), visibleTab: .closed, hasClosed: true
        ))
    }
}
