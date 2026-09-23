import Foundation
import MonacoCore

struct GroupActivityItemDTO: Codable, Equatable, Identifiable {
    let id: String
    let kind: String
    let status: String
    let symbol: String?
    let amountMicros: Int64
    let createdAt: String
    let txSignature: String?
    let tokenAmount: String?
    let proceedsUsdcMicros: String?
    let initiatedBy: String?
    let agentDisplayName: String?
    let assetKind: AssetKind?
    let tokenDecimals: Int?

    var resolvedAssetKind: AssetKind { assetKind ?? .stock }
    var resolvedTokenDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }

    init(
        id: String,
        kind: String,
        status: String,
        symbol: String?,
        amountMicros: Int64,
        createdAt: String,
        txSignature: String?,
        tokenAmount: String?,
        proceedsUsdcMicros: String?,
        initiatedBy: String?,
        agentDisplayName: String?,
        assetKind: AssetKind? = nil,
        tokenDecimals: Int? = nil
    ) {
        self.id = id
        self.kind = kind
        self.status = status
        self.symbol = symbol
        self.amountMicros = amountMicros
        self.createdAt = createdAt
        self.txSignature = txSignature
        self.tokenAmount = tokenAmount
        self.proceedsUsdcMicros = proceedsUsdcMicros
        self.initiatedBy = initiatedBy
        self.agentDisplayName = agentDisplayName
        self.assetKind = assetKind
        self.tokenDecimals = tokenDecimals
    }
}

struct GroupActivityResponse: Codable, Equatable {
    let items: [GroupActivityItemDTO]
}

struct RetryTransactionResponse: Codable, Equatable {
    let transactionId: String
    let groupId: String
    let action: String
    let status: String
    let txSignature: String?
}
