import MonacoCore
import Testing
@testable import Monaco

struct ProposalFeedPollingTests {
    private func open(_ ids: [String]) -> [ProposalDTO] {
        ids.map { ProposalDTO(id: $0, symbol: "AAPLx", status: "open", kind: "buy", canVote: true) }
    }

    @Test func theFirstTickFillsTheClosedTab() {
        #expect(ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: false, closedIsStale: false))
    }

    @Test func anOpenVoteDoesNotDragTheWholeHistoryDownEveryTick() {
        // The whole point: a five-second Open poll must not re-read the cabal's closed history.
        #expect(!ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: true, closedIsStale: false))
    }

    @Test func theClosedTabIsKeptFreshWhileItIsTheOneBeingRead() {
        #expect(ProposalFeedPolling.shouldReloadClosed(visibleTab: .closed, hasClosed: true, closedIsStale: false))
    }

    @Test func aStaleClosedTabIsReread() {
        #expect(ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: true, closedIsStale: true))
    }

    @Test func aNewProposalAloneIsNotAReasonToRereadClosed() {
        #expect(!ProposalFeedPolling.openListLostAProposal(previousOpen: open(["a"]), freshOpen: open(["a", "b"])))
    }

    @Test func aQuietTickChangesNothing() {
        #expect(!ProposalFeedPolling.openListLostAProposal(previousOpen: open(["a", "b"]), freshOpen: open(["a", "b"])))
    }

    @Test func aProposalLeavingTheOpenListMeansTheClosedTabIsStale() {
        #expect(ProposalFeedPolling.openListLostAProposal(previousOpen: open(["a", "b"]), freshOpen: open(["b"])))
    }

    @Test func theFirstTickHasNothingToCompareAgainst() {
        #expect(!ProposalFeedPolling.openListLostAProposal(previousOpen: nil, freshOpen: open(["a"])))
    }

    /// The departure signal lasts exactly one tick, so a closed read that throws used to swallow
    /// it: the next tick compares against an Open list the proposal has already left, and the
    /// Closed tab keeps a wrong count until the member switches tabs. This walks the two helpers
    /// in the order `pollTick` calls them, with the closed read failing on the tick that matters.
    @Test func aFailedClosedReadDoesNotLoseTheDeparture() {
        var previousOpen: [ProposalDTO]? = open(["a", "b"])
        var closedIsStale = false

        // Tick 1: "a" settled. The closed read is attempted and throws, so staleness survives.
        var fresh = open(["b"])
        if ProposalFeedPolling.openListLostAProposal(previousOpen: previousOpen, freshOpen: fresh) {
            closedIsStale = true
        }
        previousOpen = fresh
        #expect(ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: true, closedIsStale: closedIsStale))

        // Tick 2: nothing left the list this time, but the Closed tab is still wrong.
        fresh = open(["b"])
        if ProposalFeedPolling.openListLostAProposal(previousOpen: previousOpen, freshOpen: fresh) {
            closedIsStale = true
        }
        previousOpen = fresh
        #expect(ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: true, closedIsStale: closedIsStale))

        // The read lands: the tab is fresh again and quiet ticks stop re-reading it.
        closedIsStale = false
        #expect(!ProposalFeedPolling.shouldReloadClosed(visibleTab: .open, hasClosed: true, closedIsStale: closedIsStale))
    }
}
