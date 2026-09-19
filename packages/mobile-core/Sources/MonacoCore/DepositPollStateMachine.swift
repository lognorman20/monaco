import Foundation

/// Shared poll cadence for inbound USDC balance and fund-to-cabal sweep status.
public enum DepositPolling {
    public static let balanceInterval: Duration = .seconds(3)
    public static let sweepStatusInterval: Duration = .seconds(3)
}

public enum DepositSweepPhase: Equatable {
    case idle
    case awaitingSweep
    case credited
    case failed(String)
}

/// Tracks deposit sweep status from backend DTO `status` strings.
public struct DepositPollStateMachine: Equatable {
    public private(set) var phase: DepositSweepPhase = .idle

    public init() {}

    public mutating func apply(status: String) {
        switch status.lowercased() {
        case "pending":
            phase = .awaitingSweep
        case "confirmed":
            phase = .credited
        default:
            phase = .failed(status)
        }
    }

    public var isTerminal: Bool {
        switch phase {
        case .credited, .failed:
            true
        case .idle, .awaitingSweep:
            false
        }
    }
}

public struct DepositStatusDTO: Decodable, Equatable {
    public let depositId: String
    public let status: String
    public let shareUnits: Int64

    public init(depositId: String, status: String, shareUnits: Int64) {
        self.depositId = depositId
        self.status = status
        self.shareUnits = shareUnits
    }
}
