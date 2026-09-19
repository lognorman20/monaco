import Foundation

public struct LeaveGroupRequestDTO: Encodable, Equatable, Sendable {
    public let withdrawStake: Bool

    public init(withdrawStake: Bool) {
        self.withdrawStake = withdrawStake
    }
}

public struct WithdrawToBalanceRequestDTO: Encodable, Equatable, Sendable {
    public let shareAmountMicros: Int64?

    public init(shareAmountMicros: Int64?) {
        self.shareAmountMicros = shareAmountMicros
    }
}

public struct WithdrawToBalanceJobDTO: Codable, Equatable, Sendable {
    public let id: String
    public let status: String
    public let shareUnits: Int64
    public let sliceUsdc: Int64
    public let payoutAddress: String?

    public init(id: String, status: String, shareUnits: Int64, sliceUsdc: Int64, payoutAddress: String?) {
        self.id = id
        self.status = status
        self.shareUnits = shareUnits
        self.sliceUsdc = sliceUsdc
        self.payoutAddress = payoutAddress
    }
}
