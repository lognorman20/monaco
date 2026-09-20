import Foundation
import MonacoCore
import Testing
@testable import Monaco

struct ProposeErrorCopyTests {
    private func refusal(_ status: Int, _ message: String) -> Error {
        Monaco.MonacoAPIError.apiError(status: status, message: message)
    }

    @Test func aRouteThatVanishedIsNotReadAsTheCabalSellingMoreThanItHolds() {
        // Every 400 on the sell path used to say "The cabal doesn't hold that much anymore".
        #expect(
            ProposeErrorCopy.propose(refusal(400, "quote not routable"), isSell: true) == ProposeFlowCopy.sellTooSmall
        )
    }

    @Test func sellingMoreThanTheCabalHoldsSaysSo() {
        #expect(
            ProposeErrorCopy.propose(refusal(400, "amount exceeds treasury holding"), isSell: true)
                == ProposeFlowCopy.sellNoLongerAvailable
        )
    }

    @Test func aStockTheCabalCannotBuyNamesTheStock() {
        #expect(
            ProposeErrorCopy.propose(refusal(400, "quote not routable"), stockName: "Apple")
                == ProposeFlowCopy.cantBuyStock("Apple")
        )
    }

    @Test func aBuyOverThePotSaysTheAmountIsTooBig() {
        #expect(
            ProposeErrorCopy.propose(refusal(400, "amount exceeds treasury total available")) == ProposeFlowCopy.overPot
        )
    }

    @Test func aLongReasonSaysSoOnBuyAndOnSell() {
        #expect(ProposeErrorCopy.propose(refusal(400, "thesis exceeds maximum length")) == ProposeFlowCopy.reasonTooLong)
        #expect(
            ProposeErrorCopy.propose(refusal(400, "thesis exceeds maximum length"), isSell: true)
                == ProposeFlowCopy.reasonTooLong
        )
    }

    @Test func aRefusalTheAppDoesNotKnowFallsBackToTryAgain() {
        #expect(ProposeErrorCopy.propose(refusal(500, "internal server error")) == ProposeFlowCopy.sendFailed)
        #expect(ProposeErrorCopy.propose(Monaco.MonacoAPIError.httpStatus(400), isSell: true) == ProposeFlowCopy.sendFailed)
    }

    @Test func beingOfflineSaysToCheckTheConnection() {
        #expect(ProposeErrorCopy.propose(URLError(.notConnectedToInternet)) == ProposeFlowCopy.noConnection)
        #expect(ProposeErrorCopy.propose(URLError(.timedOut), isSell: true) == ProposeFlowCopy.noConnection)
    }
}
