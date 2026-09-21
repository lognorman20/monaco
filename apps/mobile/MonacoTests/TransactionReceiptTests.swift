import MonacoCore
import Testing
@testable import Monaco

/// The API's amountMicros is dollars for buys and sells alike; a sell's quantity comes from tokenAmount.
struct TransactionReceiptTests {
    // 12 shares, sold for $2,784.60.
    private static let twelveShares: Int64 = 1_200_000_000
    private static let proceeds: Int64 = 2_784_600_000

    private func swap(
        action: String,
        status: String,
        amountMicros: Int64,
        costBasisPrice: Int64? = nil,
        costBasisAmount: Int64? = nil,
        proceedsUsdcMicros: Int64? = nil,
        tokenAmount: Int64? = nil
    ) -> TransactionDetailDTO {
        TransactionDetailDTO(
            transactionId: "t1", groupId: "g1", action: action, status: status, amountMicros: amountMicros,
            inputToken: nil, outputToken: nil,
            inputSymbol: action == "sell" ? "AAPLx" : "USDC", outputSymbol: action == "sell" ? "USDC" : "AAPLx",
            txHash: nil, executeRequestId: nil, proposalId: nil,
            costBasisPrice: costBasisPrice, costBasisAmount: costBasisAmount,
            createdAt: "2026-09-21T15:00:00Z", confirmedAt: nil, failureReason: nil,
            proceedsUsdcMicros: proceedsUsdcMicros, tokenAmount: tokenAmount
        )
    }

    private func value(_ label: String, in receipt: TransactionReceipt) -> String? {
        receipt.rows.first { $0.label == label }?.value
    }

    @Test func confirmedSellShowsProceedsAndSharesFromTokenAmount() {
        let receipt = TransactionReceipt(transaction: swap(
            action: "sell", status: "confirmed", amountMicros: Self.proceeds,
            costBasisPrice: Self.proceeds, costBasisAmount: Self.proceeds,
            proceedsUsdcMicros: Self.proceeds, tokenAmount: Self.twelveShares
        ))
        #expect(receipt.amountMicros == Self.proceeds)
        #expect(receipt.fallbackHero == nil)
        #expect(value("Shares", in: receipt) == "12 shares")
        #expect(value("Price", in: receipt) == "\(UsdAmountFormatter.format(micros: 232_050_000)) a share")
    }

    @Test func pendingSellShowsSharesNotDollars() {
        let receipt = TransactionReceipt(transaction: swap(
            action: "sell", status: "pending", amountMicros: 0, tokenAmount: Self.twelveShares
        ))
        #expect(receipt.amountMicros == nil)
        #expect(receipt.fallbackHero == "12 shares")
    }

    @Test func sellWithoutRecordedProceedsNeverReadsCostBasisAsDollars() {
        let receipt = TransactionReceipt(transaction: swap(
            action: "sell", status: "confirmed", amountMicros: 0,
            costBasisAmount: 999_000_000, proceedsUsdcMicros: nil, tokenAmount: Self.twelveShares
        ))
        #expect(receipt.amountMicros == nil)
        #expect(receipt.fallbackHero == "12 shares")
    }

    @Test func buyShowsWhatItSpent() {
        let receipt = TransactionReceipt(transaction: swap(
            action: "buy", status: "confirmed", amountMicros: Self.proceeds,
            costBasisPrice: Self.proceeds, costBasisAmount: Self.twelveShares
        ))
        #expect(receipt.amountMicros == Self.proceeds)
        #expect(value("Shares", in: receipt) == "12 shares")
    }
}
