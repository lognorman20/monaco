import Foundation
import MonacoCore

struct TransactionDetailDTO: Codable, Equatable {
    let transactionId: String
    let groupId: String
    let action: String
    let status: String
    let amountMicros: Int64
    let inputMint: String?
    let outputMint: String?
    let inputSymbol: String?
    let outputSymbol: String?
    let txSignature: String?
    let executeRequestId: String?
    let proposalId: String?
    let costBasisPrice: Int64?
    let costBasisAmount: Int64?
    let createdAt: String
    let confirmedAt: String?
    let failureReason: String?
    let proceedsUsdcMicros: Int64?
    let assetKind: AssetKind?
    let tokenDecimals: Int?

    var resolvedAssetKind: AssetKind { assetKind ?? .stock }
    var resolvedTokenDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }

    init(
        transactionId: String,
        groupId: String,
        action: String,
        status: String,
        amountMicros: Int64,
        inputMint: String?,
        outputMint: String?,
        inputSymbol: String?,
        outputSymbol: String?,
        txSignature: String?,
        executeRequestId: String?,
        proposalId: String?,
        costBasisPrice: Int64?,
        costBasisAmount: Int64?,
        createdAt: String,
        confirmedAt: String?,
        failureReason: String?,
        proceedsUsdcMicros: Int64?,
        assetKind: AssetKind? = nil,
        tokenDecimals: Int? = nil
    ) {
        self.transactionId = transactionId
        self.groupId = groupId
        self.action = action
        self.status = status
        self.amountMicros = amountMicros
        self.inputMint = inputMint
        self.outputMint = outputMint
        self.inputSymbol = inputSymbol
        self.outputSymbol = outputSymbol
        self.txSignature = txSignature
        self.executeRequestId = executeRequestId
        self.proposalId = proposalId
        self.costBasisPrice = costBasisPrice
        self.costBasisAmount = costBasisAmount
        self.createdAt = createdAt
        self.confirmedAt = confirmedAt
        self.failureReason = failureReason
        self.proceedsUsdcMicros = proceedsUsdcMicros
        self.assetKind = assetKind
        self.tokenDecimals = tokenDecimals
    }
}
