import Foundation
import MonacoCore
import Testing
@testable import Monaco

// The app target shadows this MonacoCore DTO; pin the tests to the one the screens use.
private typealias PlatformBalanceDTO = Monaco.PlatformBalanceDTO

private let ownAddress = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
private let outsideAddress = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"

/// The screens' types are main-actor isolated (the app's default), so their fixtures are too.
@MainActor
private enum Fixture {
    static func balance(_ micros: Int64, pending: Int64 = 0) -> PlatformBalanceDTO {
        PlatformBalanceDTO(availableUsdcMicros: micros, memberWalletAddress: ownAddress, pendingAllocationMicros: pending)
    }
}

/// Fund this cabal: what the amount step says and allows against the balance it has.
@MainActor
struct FundCabalFormTests {
    @Test func theButtonReadsTheAmountOnceThereIsOne() {
        #expect(FundCabalForm(amountText: "", balance: Fixture.balance(248_500_000)).ctaTitle == "Add money")
        #expect(FundCabalForm(amountText: "0", balance: Fixture.balance(248_500_000)).ctaTitle == "Add money")
        #expect(FundCabalForm(amountText: "50", balance: Fixture.balance(248_500_000)).ctaTitle == "Add $50 to the pot")
    }

    @Test func anAmountOverTheBalanceCannotBeSent() {
        #expect(FundCabalForm(amountText: "248.50", balance: Fixture.balance(248_500_000)).canSubmit)
        #expect(!FundCabalForm(amountText: "248.51", balance: Fixture.balance(248_500_000)).canSubmit)
        #expect(!FundCabalForm(amountText: "", balance: Fixture.balance(248_500_000)).canSubmit)
        #expect(!FundCabalForm(amountText: "10", balance: Fixture.balance(0)).canSubmit)
        #expect(!FundCabalForm(amountText: "10", balance: nil).canSubmit)
    }

    /// The helper under the figure says what there is to fund with, and what is already on
    /// its way to a cabal, rather than the old "From your account balance" with no figure.
    @Test func theHelperSaysWhatIsAvailable() {
        #expect(FundCabalForm(amountText: "", balance: Fixture.balance(248_500_000)).availability == "$248.50 available")
        #expect(
            FundCabalForm(amountText: "", balance: Fixture.balance(198_500_000, pending: 50_000_000)).availability
                == "$198.50 available · $50.00 funding a cabal"
        )
        #expect(FundCabalForm(amountText: "", balance: nil).availability == nil)
    }

    /// With a cabal to pick, the picker sits under the pad, so the line under the figure is
    /// what says where the money goes while the member types.
    @Test func theNoteNamesTheCabalWhenThereIsAChoice() {
        #expect(FundCabalForm.note(into: nil)
            == "The money leaves your account balance and joins the pot. Your slice grows by the same amount.")
        #expect(FundCabalForm.note(into: "Semis or bust")
            == "The money leaves your account balance and joins the Semis or bust pot. Your slice grows by the same amount.")
        #expect(FundCabalForm.note(into: "") == FundCabalForm.note(into: nil))
    }

    @Test func maxIsTheWholeBalanceAndNothingWhenEmpty() {
        #expect(FundCabalForm(amountText: "", balance: Fixture.balance(248_500_000)).maxDollars == Decimal(string: "248.5"))
        #expect(FundCabalForm(amountText: "", balance: Fixture.balance(0)).maxDollars == nil)
    }
}

/// Which state Fund this cabal is in. The amount pad and its button only ever show together.
@MainActor
struct FundCabalStageTests {
    @Test func aFirstLoadIsLoadingOrItsFailure() {
        #expect(FundCabalStage.resolve(phase: .loading, hasCabals: true) == .loading)
        #expect(FundCabalStage.resolve(phase: .failed("No connection. Check your internet."), hasCabals: true)
            == .failed("No connection. Check your internet."))
    }

    @Test func noCabalWinsOverAnyBalance() {
        #expect(FundCabalStage.resolve(phase: .loaded(Fixture.balance(248_500_000)), hasCabals: false) == .noCabals)
    }

    @Test func anEmptyBalanceAsksForMoneyFirst() {
        let empty = Fixture.balance(0)
        #expect(FundCabalStage.resolve(phase: .loaded(empty), hasCabals: true) == .needsMoney(empty))
        #expect(!FundCabalStage.needsMoney(empty).showsAmountEntry)
    }

    @Test func aBalanceShowsTheAmountPad() {
        let funded = Fixture.balance(248_500_000)
        let stage = FundCabalStage.resolve(phase: .loaded(funded), hasCabals: true)
        #expect(stage == .amount(funded))
        #expect(stage.showsAmountEntry)
    }
}

/// Cash out to an address: when Continue is live, and what the field says about an address.
@MainActor
struct WithdrawFormTests {
    @Test func continueNeedsAnAmountWithinTheBalanceAndAUsableAddress() {
        #expect(WithdrawForm(amountText: "100", destinationAddress: outsideAddress, balance: Fixture.balance(248_500_000)).canContinue)
        #expect(!WithdrawForm(amountText: "", destinationAddress: outsideAddress, balance: Fixture.balance(248_500_000)).canContinue)
        #expect(!WithdrawForm(amountText: "300", destinationAddress: outsideAddress, balance: Fixture.balance(248_500_000)).canContinue)
        #expect(!WithdrawForm(amountText: "100", destinationAddress: "", balance: Fixture.balance(248_500_000)).canContinue)
        #expect(!WithdrawForm(amountText: "100", destinationAddress: ownAddress, balance: Fixture.balance(248_500_000)).canContinue)
    }

    /// Nothing is said about an empty field; a pasted address that can't be used says why.
    @Test func theFieldOnlySpeaksUpAboutAnAddressItCannotUse() {
        #expect(WithdrawForm(amountText: "", destinationAddress: "", balance: Fixture.balance(1)).addressProblem == nil)
        #expect(WithdrawForm(amountText: "", destinationAddress: outsideAddress, balance: Fixture.balance(1)).addressProblem == nil)
        #expect(WithdrawForm(amountText: "", destinationAddress: "0xabc", balance: Fixture.balance(1)).addressProblem != nil)
        #expect(WithdrawForm(amountText: "", destinationAddress: ownAddress, balance: Fixture.balance(1)).addressProblem != nil)
    }

    @Test func theHelperSaysWhatIsAvailable() {
        #expect(WithdrawForm(amountText: "", destinationAddress: "", balance: Fixture.balance(248_500_000)).balanceHelper == "$248.50 available")
    }
}

/// The deposit address card's three states, from the three values the load leaves behind.
@MainActor
struct DepositAddressCardContentTests {
    @Test func aLoadInFlightWinsThenAnAddressThenWhatWentWrong() {
        #expect(DepositAddressCard.Content.resolve(isLoading: true, address: ownAddress, errorMessage: nil) == .loading)
        #expect(DepositAddressCard.Content.resolve(isLoading: false, address: ownAddress, errorMessage: nil) == .ready(ownAddress))
        #expect(DepositAddressCard.Content.resolve(isLoading: false, address: nil, errorMessage: "Couldn't load your deposit address.")
            == .unavailable("Couldn't load your deposit address."))
    }

    /// A cancelled load says nothing went wrong, and the card still offers a way on.
    @Test func noAddressAndNoReasonIsNotReadyYet() {
        #expect(DepositAddressCard.Content.resolve(isLoading: false, address: nil, errorMessage: nil)
            == .unavailable("Deposit address not ready yet."))
    }
}

/// The receipt's words and facts, from the DTOs the screen loads.
@MainActor
struct TransactionReceiptRedesignTests {
    private var boughtApple: TransactionDetailDTO {
        TransactionDetailDTO(
            transactionId: "t5", groupId: "g1", action: "buy", status: "confirmed", amountMicros: 250_000_000,
            inputMint: nil, outputMint: nil, inputSymbol: "USDC", outputSymbol: "AAPLx",
            txSignature: "4mZaQ8nJv2kPp7sWfLr3bXy9TcHd6eUoGi1AqRsNmVtK", executeRequestId: nil, proposalId: "p1",
            costBasisPrice: 250_000_000, costBasisAmount: 108_034_000, createdAt: "2026-09-16T14:02:00Z",
            confirmedAt: "2026-09-16T14:02:09Z", failureReason: nil, proceedsUsdcMicros: nil
        )
    }

    private func deposit(_ status: String) -> GetDepositResponse {
        GetDepositResponse(
            depositId: "d1", groupId: "g1", amount: 100_000_000, status: status, fromAddress: nil,
            txSignature: nil, shareUnits: 0, createdAt: "2026-09-18T11:40:00Z"
        )
    }

    /// The label says what the fact is, so the value is only the figure.
    @Test func aBuyListsSharesAndThePriceAShareAsFigures() {
        let receipt = TransactionReceipt(transaction: boughtApple)
        #expect(receipt.headline == "Bought Apple")
        #expect(receipt.rows.map(\.label) == ["Shares", "Price a share", "Date"])
        #expect(receipt.rows[0].value == "1.0803")
        #expect(receipt.rows[1].value == "$231.41")
        #expect(receipt.solscanURL != nil)
    }

    /// A deposit still on its way into the pot is not headed "Money added".
    @Test func aDepositsHeadlineFollowsItsState() {
        #expect(TransactionReceipt(deposit: deposit("confirmed")).headline == "Money added")
        #expect(TransactionReceipt(deposit: deposit("pending")).headline == "Adding money")
        #expect(TransactionReceipt(deposit: deposit("failed")).headline == "Couldn't add money")
        #expect(TransactionReceipt(deposit: deposit("pending")).status == .pending)
    }

    @Test func sharesDropTrailingZeros() {
        #expect(TransactionReceipt.sharesFigure(1.08034) == "1.0803")
        #expect(TransactionReceipt.sharesFigure(1) == "1")
        #expect(TransactionReceipt.sharesFigure(0.25) == "0.25")
        #expect(TransactionReceipt.sharesFigure(10) == "10")
        #expect(TransactionReceipt.sharesFigure(100.5) == "100.5")
    }
}

/// Preset chips take two rows only at the accessibility sizes, each chip exactly once.
@MainActor
struct AmountEntryPresetRowTests {
    @Test func oneRowNormally() {
        #expect(AmountEntryText.presetRows(4, stacked: false) == [[0, 1, 2, 3]])
    }

    @Test func twoRowsAtTheAccessibilitySizesLongerHalfFirst() {
        #expect(AmountEntryText.presetRows(4, stacked: true) == [[0, 1], [2, 3]])
        #expect(AmountEntryText.presetRows(3, stacked: true) == [[0, 1], [2]])
    }

    @Test func twoChipsOrFewerNeverSplit() {
        #expect(AmountEntryText.presetRows(2, stacked: true) == [[0, 1]])
        #expect(AmountEntryText.presetRows(1, stacked: true) == [[0]])
        #expect(AmountEntryText.presetRows(0, stacked: true).isEmpty)
    }
}

/// The copy these screens added speaks the product's words, not the backend's.
@MainActor
struct MoneyFlowCopyTests {
    @Test func theNewCopyPassesTheMainFlowAudit() {
        let strings = DepositContent.steps + [
            DepositAddressCard.networkNote,
            FundCabalForm.note(into: nil),
            FundCabalForm.note(into: "Weekend investors"),
            WithdrawForm.caveat,
            PlatformBalanceCard.pendingLine(micros: 50_000_000) ?? "",
        ]
        #expect(MainFlowCopyAudit.stringsAreClean(strings))
    }

    @Test func thePendingLineOnlyShowsWhenSomethingIsOnItsWay() {
        #expect(PlatformBalanceCard.pendingLine(micros: 0) == nil)
        #expect(PlatformBalanceCard.pendingLine(micros: 50_000_000) == "$50.00 funding a cabal")
    }
}
