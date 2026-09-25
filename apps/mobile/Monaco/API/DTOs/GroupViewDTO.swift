import Foundation
import MonacoCore

struct PotRowDTO: Codable, Equatable, Identifiable {
    let symbol: String
    let units: String
    let markUsd: String
    let valueUsd: String
    let dollarPnl: String
    let afterHours: Bool?
    let tokenAmount: String?
    let assetKind: AssetKind?
    let tokenDecimals: Int?
    let premiumBps: Int?
    let uiAmountMultiplier: String?
    let issuerName: String?

    var id: String { symbol }
    var resolvedAssetKind: AssetKind { assetKind ?? .stock }
    var resolvedTokenDecimals: Int { tokenDecimals ?? AssetCatalogDefaults.decimals }
    var resolvedUiMultiplier: Decimal {
        guard let uiAmountMultiplier, let value = Decimal(string: uiAmountMultiplier, locale: Locale(identifier: "en_US_POSIX")), value > 0 else {
            return 1
        }
        return value
    }

    init(
        symbol: String,
        units: String,
        markUsd: String,
        valueUsd: String,
        dollarPnl: String,
        afterHours: Bool?,
        tokenAmount: String? = nil,
        assetKind: AssetKind? = nil,
        tokenDecimals: Int? = nil,
        premiumBps: Int? = nil,
        uiAmountMultiplier: String? = nil,
        issuerName: String? = nil
    ) {
        self.symbol = symbol
        self.units = units
        self.markUsd = markUsd
        self.valueUsd = valueUsd
        self.dollarPnl = dollarPnl
        self.afterHours = afterHours
        self.tokenAmount = tokenAmount
        self.assetKind = assetKind
        self.tokenDecimals = tokenDecimals
        self.premiumBps = premiumBps
        self.uiAmountMultiplier = uiAmountMultiplier
        self.issuerName = issuerName
    }
}

struct MemberSliceDTO: Codable, Equatable {
    let shareUnits: String
    let equityUsd: String
    let slicePercent: String
    let dollarPnl: String
    let percentReturn: String?
}

struct LeaderboardRowDTO: Codable, Equatable, Identifiable {
    let rank: Int
    let userId: String
    let displayName: String
    var profilePhotoUrl: String? = nil
    let percentReturn: String?
    let dollarPnl: String

    var id: String { userId }
}

struct GroupAgentDTO: Codable, Equatable {
    let id: String
    let status: String
    let agentDisplayName: String
    let allocationUsdcMicros: String
    let apiKey: String?
}

struct GroupViewDTO: Codable, Equatable {
    let id: String
    let name: String
    let treasuryAddress: String?
    let potTotalUsd: String?
    let pot: [PotRowDTO]
    let you: MemberSliceDTO
    let members: [LeaderboardRowDTO]
    let proposals: [ProposalDTO]?
    let agent: GroupAgentDTO?

    var resolvedPotTotalUsd: String {
        if let potTotalUsd, !potTotalUsd.isEmpty {
            return potTotalUsd
        }
        let sum = pot.reduce(Decimal.zero) { partial, row in
            partial + (Decimal(string: row.valueUsd) ?? .zero)
        }
        var rounded = sum
        var result = Decimal()
        NSDecimalRound(&result, &rounded, 2, .plain)
        return NSDecimalNumber(decimal: result).stringValue
    }
}
