import Foundation
import MonacoCore
import Testing
@testable import Monaco

struct ProposeErrorCopyTests {
    private func refusal(_ status: Int, _ message: String) -> Error {
        Monaco.MonacoAPIError.apiError(status: status, message: message)
    }

    // MARK: Sending the proposal

    @Test func aRouteThatVanishedIsNotReadAsAnAmountTheMemberShouldChange() {
        // The backend says "quote not routable" for three different things — no route, below the
        // minimum size, and a quote that came back unroutable — with nothing to tell them apart.
        // On the sell path the amount was quoted routable seconds earlier, so this is a route that
        // went away. Neither "the cabal doesn't hold that much" (what every sell 400 used to say)
        // nor "try a bigger amount" is true, and both send the member back to the same trade.
        let copy = ProposeErrorCopy.propose(refusal(400, "quote not routable"), isSell: true)
        #expect(copy == ProposeFlowCopy.sendFailed)
        #expect(copy != ProposeFlowCopy.sellTooSmall)
        #expect(copy != ProposeFlowCopy.sellNoLongerAvailable)
    }

    @Test func sellingMoreThanTheCabalHoldsSaysSo() {
        #expect(
            ProposeErrorCopy.propose(refusal(400, "amount exceeds treasury holding"), isSell: true)
                == ProposeFlowCopy.sellNoLongerAvailable
        )
    }

    @Test func aBuyIsNeverToldTheCabalSoldDownItsHolding() {
        // Only a sell is checked against the holding. If a buy ever comes back with this message
        // the app cannot read it, and telling a member buying a stock that "the cabal doesn't hold
        // that much anymore" is advice about a trade they are not making.
        let buy = ProposeErrorCopy.propose(refusal(400, "amount exceeds treasury holding"))
        #expect(buy == ProposeFlowCopy.sendFailed)
        #expect(buy != ProposeFlowCopy.sellNoLongerAvailable)

        let buyQuote = ProposeErrorCopy.quote(refusal(400, "amount exceeds treasury holding"))
        #expect(buyQuote == ProposeFlowCopy.priceCheckFailed)
        #expect(buyQuote != ProposeFlowCopy.sellNoLongerAvailable)
    }

    @Test func aStockTheCabalCannotBuyNamesTheStock() {
        #expect(
            ProposeErrorCopy.propose(refusal(400, "quote not routable"), stockName: "Apple")
                == ProposeFlowCopy.cantBuyStock("Apple")
        )
    }

    @Test func aBuyThatCannotRouteWithNoStockNameStillFallsBack() {
        #expect(ProposeErrorCopy.propose(refusal(400, "quote not routable")) == ProposeFlowCopy.sendFailed)
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

    // MARK: Checking the price

    @Test func aSellPriceCheckOverTheHoldingSaysWhatWentWrong() {
        // Review is enabled while the member is over the holding, so the quote comes back refused
        // with the same sentence the propose endpoint uses. "Couldn't check the price. Try again"
        // is advice that fails identically on every retry.
        #expect(
            ProposeErrorCopy.quote(refusal(400, "amount exceeds treasury holding"), isSell: true)
                == ProposeFlowCopy.sellNoLongerAvailable
        )
    }

    @Test func aPriceCheckThatJustFailedSaysSo() {
        #expect(ProposeErrorCopy.quote(refusal(500, "internal server error")) == ProposeFlowCopy.priceCheckFailed)
        #expect(ProposeErrorCopy.quote(Monaco.MonacoAPIError.httpStatus(503)) == ProposeFlowCopy.priceCheckFailed)
        #expect(
            ProposeErrorCopy.quote(refusal(400, "quote not routable"), isSell: true) == ProposeFlowCopy.priceCheckFailed
        )
    }

    @Test func aPriceCheckOfflineSaysToCheckTheConnection() {
        #expect(ProposeErrorCopy.quote(URLError(.notConnectedToInternet)) == ProposeFlowCopy.noConnection)
        #expect(ProposeErrorCopy.quote(URLError(.timedOut), isSell: true) == ProposeFlowCopy.noConnection)
    }

    @Test func aBuyPriceCheckIsUnchangedByTheSharedMapping() {
        // The buy quote endpoint answers an unroutable amount with 200 and `routable: false`, and
        // never with the treasury sentences, so nothing on that path moved.
        #expect(ProposeErrorCopy.quote(refusal(404, "symbol not found")) == ProposeFlowCopy.priceCheckFailed)
    }
}
