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
}
