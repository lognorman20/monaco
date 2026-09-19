import Foundation

public struct RedeemJobDTO: Codable, Equatable, Sendable {
    public let id: String
    public let status: String

    public init(id: String, status: String) {
        self.id = id
        self.status = status
    }
}

public struct RedeemRequestDTO: Encodable, Equatable, Sendable {
    public let shareUnits: String
    public let payoutAddress: String
    public let payoutProof: String

    public init(shareUnits: String, payoutAddress: String, payoutProof: String) {
        self.shareUnits = shareUnits
        self.payoutAddress = payoutAddress
        self.payoutProof = payoutProof
    }
}

/// Minimum partial withdraw slice in USDC micros ($0.10); matches backend RedeemDustMinimumMicros.
public enum RedeemDustMinimum {
    public static let usdcMicros: Int64 = 100_000
}

public struct RedeemSliderGate {
    public let dustMinimumMicros: Int64

    public init(dustMinimumMicros: Int64 = RedeemDustMinimum.usdcMicros) {
        self.dustMinimumMicros = dustMinimumMicros
    }

    public func maySubmit(selectedMicros: Int64) -> Bool {
        selectedMicros >= dustMinimumMicros
    }
}

public struct RedeemBoardRefreshPlan: Equatable, Sendable {
    public let refreshGroup: Bool
    public let refreshHome: Bool

    public init(refreshGroup: Bool = true, refreshHome: Bool = true) {
        self.refreshGroup = refreshGroup
        self.refreshHome = refreshHome
    }

    public static func afterSuccess() -> RedeemBoardRefreshPlan {
        RedeemBoardRefreshPlan(refreshGroup: true, refreshHome: true)
    }
}
