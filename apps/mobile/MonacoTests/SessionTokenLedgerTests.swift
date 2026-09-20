import Testing
@testable import Monaco

/// The guard every token-rejection path consults: `refreshedAccessToken`,
/// `signOutAfterRejectedSession(rejectedToken:)` and `signOut(reason:rejectedToken:)` all
/// act only when the rejected token still belongs to the open session.
struct SessionTokenLedgerTests {
    @Test func aTokenTheSessionAdoptedIsRecognised() {
        var ledger = SessionTokenLedger()
        ledger.adopt("token-a")

        #expect(ledger.contains("token-a"))
        #expect(!ledger.contains("token-b"))
    }

    @Test func aFreshTokenDoesNotDisownTheOneItReplaced() {
        var ledger = SessionTokenLedger()
        ledger.adopt("token-a")
        ledger.adopt("token-b")

        // A request sent just before the rotation is still this session's.
        #expect(ledger.contains("token-a"))
        #expect(ledger.contains("token-b"))
    }

    /// The point of the guard: after a sign-out nothing belongs to the session any more, so
    /// a 401 that was still in flight cannot sign out whoever signs in next.
    @Test func endingTheSessionDisownsEveryToken() {
        var ledger = SessionTokenLedger()
        ledger.adopt("token-a")
        ledger.adopt("token-b")
        ledger.clear()

        #expect(!ledger.contains("token-a"))
        #expect(!ledger.contains("token-b"))
        #expect(ledger.isEmpty)
    }

    /// Privy rotates roughly hourly. Keeping every token a multi-day session ever used would
    /// leave dozens of live credentials in the heap; only the current one and the one it
    /// replaced can legitimately be named by a reply still in flight.
    @Test func onlyTheCurrentAndPreviousTokensAreKept() {
        var ledger = SessionTokenLedger()
        ledger.adopt("token-a")
        ledger.adopt("token-b")
        ledger.adopt("token-c")

        #expect(!ledger.contains("token-a"))
        #expect(ledger.contains("token-b"))
        #expect(ledger.contains("token-c"))
    }

    /// A refresh that hands back the token we already hold must not push the previous one
    /// out: requests carrying it can still be in flight.
    @Test func readoptingTheCurrentTokenChangesNothing() {
        var ledger = SessionTokenLedger()
        ledger.adopt("token-a")
        ledger.adopt("token-b")
        ledger.adopt("token-b")

        #expect(ledger.contains("token-a"))
        #expect(ledger.contains("token-b"))
    }

    @Test func aFreshLedgerRecognisesNothing() {
        let ledger = SessionTokenLedger()

        #expect(ledger.isEmpty)
        #expect(!ledger.contains("token-a"))
    }
}
